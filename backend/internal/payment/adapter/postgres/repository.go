package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	ledgerdomain "github.com/mesob-delivery/backend/internal/ledger/domain"
	ledgerusecase "github.com/mesob-delivery/backend/internal/ledger/usecase"
	"github.com/mesob-delivery/backend/internal/payment/domain"
)

type Repository struct {
	pool   *pgxpool.Pool
	ledger *ledgerusecase.Service
}

func NewRepository(pool *pgxpool.Pool, ledger ...*ledgerusecase.Service) *Repository {
	repo := &Repository{pool: pool}
	if len(ledger) > 0 {
		repo.ledger = ledger[0]
	}
	return repo
}

type ledgerTransaction struct{ pgx.Tx }

func (t ledgerTransaction) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.Tx.Exec(ctx, sql, args...)
	return err
}

func (t ledgerTransaction) QueryRow(ctx context.Context, sql string, args ...any) ledgerusecase.Row {
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (r *Repository) ResolveProvider(ctx context.Context, userID, orderID string) (domain.Provider, error) {
	var method string
	err := r.pool.QueryRow(ctx, `SELECT payment_method FROM orders WHERE id=$1 AND user_id=$2`, orderID, userID).Scan(&method)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrPaymentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve order payment method: %w", err)
	}
	provider := domain.Provider(strings.ToUpper(method))
	if !provider.Valid() {
		return "", domain.ErrPaymentState
	}
	return provider, nil
}

func (r *Repository) CreateIntent(ctx context.Context, userID, orderID string, provider domain.Provider) (domain.Transaction, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("begin payment intent: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var method, status string
	var amount int64
	err = tx.QueryRow(ctx, `SELECT payment_method,payment_status,total_minor FROM orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, orderID, userID).Scan(&method, &status, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transaction{}, domain.ErrPaymentNotFound
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("load payable order: %w", err)
	}
	if strings.ToUpper(method) != string(provider) || status != "pending_payment" {
		return domain.Transaction{}, domain.ErrPaymentState
	}
	var payment domain.Transaction
	err = tx.QueryRow(ctx, `SELECT order_id::text,provider,reference,amount_minor,currency,status,COALESCE(checkout_url,'') FROM payment_transactions WHERE order_id=$1 AND provider=$2 FOR UPDATE`, orderID, string(provider)).Scan(&payment.OrderID, &payment.Provider, &payment.Reference, &payment.AmountMinor, &payment.Currency, &payment.Status, &payment.CheckoutURL)
	if errors.Is(err, pgx.ErrNoRows) {
		payment = domain.Transaction{OrderID: orderID, Provider: provider, Reference: orderID, AmountMinor: amount, Currency: "ETB", Status: domain.Pending}
		_, err = tx.Exec(ctx, `INSERT INTO payment_transactions(order_id,provider,reference,amount_minor,currency,status) VALUES($1,$2,$3,$4,'ETB','pending')`, orderID, string(provider), payment.Reference, amount)
		if err != nil {
			return domain.Transaction{}, fmt.Errorf("create payment transaction: %w", err)
		}
	} else if err != nil {
		return domain.Transaction{}, fmt.Errorf("load payment transaction: %w", err)
	}
	if payment.Status != domain.Pending {
		return domain.Transaction{}, domain.ErrPaymentState
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Transaction{}, fmt.Errorf("commit payment intent: %w", err)
	}
	return payment, nil
}

func (r *Repository) UpdateInitialized(ctx context.Context, reference string, result domain.InitializeResult) error {
	command, err := r.pool.Exec(ctx, `UPDATE payment_transactions SET checkout_url=$2,gateway_reference=NULLIF($3,''),updated_at=now() WHERE reference=$1 AND status='pending'`, reference, result.CheckoutURL, result.GatewayReference)
	if err != nil {
		return fmt.Errorf("save gateway initialization: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ErrPaymentState
	}
	return nil
}

func (r *Repository) GetTransaction(ctx context.Context, provider domain.Provider, reference string) (domain.Transaction, error) {
	var payment domain.Transaction
	err := r.pool.QueryRow(ctx, `SELECT order_id::text,provider,reference,amount_minor,currency,status,COALESCE(checkout_url,'') FROM payment_transactions WHERE provider=$1 AND reference=$2`, string(provider), reference).Scan(&payment.OrderID, &payment.Provider, &payment.Reference, &payment.AmountMinor, &payment.Currency, &payment.Status, &payment.CheckoutURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transaction{}, domain.ErrPaymentNotFound
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("load payment transaction: %w", err)
	}
	return payment, nil
}

func (r *Repository) Confirm(ctx context.Context, provider domain.Provider, reference string, amount int64, currency string) (string, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, fmt.Errorf("begin payment confirmation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var orderID, expectedCurrency, status string
	var expectedAmount int64
	err = tx.QueryRow(ctx, `SELECT order_id::text,amount_minor,currency,status FROM payment_transactions WHERE provider=$1 AND reference=$2 FOR UPDATE`, string(provider), reference).Scan(&orderID, &expectedAmount, &expectedCurrency, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, domain.ErrPaymentNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("lock payment transaction: %w", err)
	}
	if status == string(domain.Paid) {
		return orderID, false, nil
	}
	if status != string(domain.Pending) || amount != expectedAmount || !strings.EqualFold(strings.TrimSpace(currency), strings.TrimSpace(expectedCurrency)) {
		return "", false, domain.ErrInvalidPayment
	}
	var restaurantID string
	var subtotal, commission, deliveryFee, total int64
	err = tx.QueryRow(ctx, `SELECT restaurant_id::text,subtotal_minor,commission_minor,delivery_fee_minor,total_minor
FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&restaurantID, &subtotal, &commission, &deliveryFee, &total)
	if err != nil {
		return "", false, fmt.Errorf("load order payment split: %w", err)
	}
	if total != expectedAmount {
		return "", false, domain.ErrInvalidPayment
	}
	command, err := tx.Exec(ctx, `UPDATE orders SET payment_status='paid',status='PAID',updated_at=now() WHERE id=$1 AND payment_status='pending_payment'`, orderID)
	if err != nil {
		return "", false, fmt.Errorf("mark order paid: %w", err)
	}
	if command.RowsAffected() != 1 {
		return "", false, domain.ErrPaymentState
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_transactions SET status='paid',updated_at=now() WHERE provider=$1 AND reference=$2`, string(provider), reference); err != nil {
		return "", false, fmt.Errorf("mark payment paid: %w", err)
	}
	entry := ledgerdomain.OnlinePaymentEntry(orderID, string(provider), reference, restaurantID, total, subtotal, commission, deliveryFee)
	if r.ledger == nil {
		return "", false, errors.New("ledger service is not configured")
	}
	if err := r.ledger.Record(ctx, ledgerTransaction{tx}, entry); err != nil {
		return "", false, fmt.Errorf("post online payment ledger entry: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"provider": provider, "reference": reference})
	if _, err := tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'restaurant.order_paid',$2::jsonb) ON CONFLICT(order_id,event_type) DO NOTHING`, orderID, string(payload)); err != nil {
		return "", false, fmt.Errorf("write paid event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("commit payment confirmation: %w", err)
	}
	return orderID, true, nil
}

func (r *Repository) FailAndRestore(ctx context.Context, provider domain.Provider, reference string) (string, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, fmt.Errorf("begin payment failure: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var orderID, status string
	err = tx.QueryRow(ctx, `SELECT order_id::text,status FROM payment_transactions WHERE provider=$1 AND reference=$2 FOR UPDATE`, string(provider), reference).Scan(&orderID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, domain.ErrPaymentNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("lock failed payment: %w", err)
	}
	if status == string(domain.Paid) || status == string(domain.Failed) {
		return orderID, false, nil
	}
	command, err := tx.Exec(ctx, `UPDATE orders SET payment_status='payment_failed',status='cancelled',updated_at=now() WHERE id=$1 AND payment_status='pending_payment'`, orderID)
	if err != nil {
		return "", false, fmt.Errorf("mark order payment failed: %w", err)
	}
	if command.RowsAffected() != 1 {
		return "", false, domain.ErrPaymentState
	}
	_, err = tx.Exec(ctx, `UPDATE menu_items mi SET stock_remaining=LEAST(mi.stock_limit,COALESCE(mi.stock_remaining,mi.stock_limit)+oi.quantity),updated_at=now() FROM (SELECT menu_item_id,SUM(quantity)::integer AS quantity FROM order_items WHERE order_id=$1 AND menu_item_id IS NOT NULL GROUP BY menu_item_id) oi WHERE mi.id=oi.menu_item_id AND mi.stock_limit IS NOT NULL`, orderID)
	if err != nil {
		return "", false, fmt.Errorf("restore failed order inventory: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_transactions SET status='failed',updated_at=now() WHERE provider=$1 AND reference=$2`, string(provider), reference); err != nil {
		return "", false, fmt.Errorf("mark payment failed: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"provider": provider, "reference": reference})
	if _, err := tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'restaurant.order_payment_failed',$2::jsonb) ON CONFLICT(order_id,event_type) DO NOTHING`, orderID, string(payload)); err != nil {
		return "", false, fmt.Errorf("write failed payment event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("commit payment failure: %w", err)
	}
	return orderID, true, nil
}
