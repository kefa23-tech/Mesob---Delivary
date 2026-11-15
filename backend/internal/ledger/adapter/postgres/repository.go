package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	ledgerdomain "github.com/mesob-delivery/backend/internal/ledger/domain"
	ledgerusecase "github.com/mesob-delivery/backend/internal/ledger/usecase"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Record appends an immutable journal entry inside its caller's transaction.
// The database's deferred constraint trigger independently verifies balance.
func (r *Repository) Record(ctx context.Context, tx ledgerusecase.Transaction, entry ledgerdomain.Entry) error {
	_, err := recordTx(ctx, tx, entry)
	return err
}

func recordTx(ctx context.Context, tx ledgerusecase.Transaction, entry ledgerdomain.Entry) (bool, error) {
	if err := entry.Validate(); err != nil {
		return false, err
	}
	var entryID string
	err := tx.QueryRow(ctx, `INSERT INTO ledger_entries(source_key,order_id,entry_type,currency,description)
VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5) ON CONFLICT(source_key) DO NOTHING RETURNING id::text`,
		entry.SourceKey, entry.OrderID, entry.EntryType, entry.Currency, entry.Description).Scan(&entryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // the source event has already been journaled
	}
	if err != nil {
		return false, fmt.Errorf("create ledger entry: %w", err)
	}
	for i, line := range entry.Lines {
		var accountID string
		err := tx.QueryRow(ctx, `INSERT INTO ledger_accounts(account_key,account_type,owner_user_id,owner_restaurant_id,currency)
VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5)
ON CONFLICT(account_key) DO UPDATE SET account_key=EXCLUDED.account_key
RETURNING id::text`, line.AccountKey, string(line.AccountType), line.OwnerUserID, line.OwnerRestaurantID, entry.Currency).Scan(&accountID)
		if err != nil {
			return false, fmt.Errorf("resolve ledger account %q: %w", line.AccountKey, err)
		}
		if err := tx.Exec(ctx, `INSERT INTO ledger_lines(entry_id,line_number,account_id,amount_minor) VALUES($1,$2,$3,$4)`, entryID, i+1, accountID, line.AmountMinor); err != nil {
			return false, fmt.Errorf("append ledger line %d: %w", i+1, err)
		}
	}
	return true, nil
}

// RecordWithinTransaction appends an entry using an existing PostgreSQL transaction.
func RecordWithinTransaction(ctx context.Context, tx pgx.Tx, entry ledgerdomain.Entry) error {
	_, err := recordTx(ctx, pgxTransaction{tx}, entry)
	return err
}

type pgxTransaction struct{ pgx.Tx }

func (t pgxTransaction) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.Tx.Exec(ctx, sql, args...)
	return err
}

func (t pgxTransaction) QueryRow(ctx context.Context, sql string, args ...any) ledgerusecase.Row {
	return t.Tx.QueryRow(ctx, sql, args...)
}

// RecordCourierRemittance settles cash received from a courier and reduces
// both their outstanding cash liability and the receivable ledger account.
func (r *Repository) RecordCourierRemittance(ctx context.Context, courierID, reference string, amountMinor int64) error {
	return r.RecordCourierRemittanceForAdmin(ctx, courierID, reference, amountMinor, "")
}

// RecordCourierRemittanceForAdmin records remittance and its admin audit row atomically.
func (r *Repository) RecordCourierRemittanceForAdmin(ctx context.Context, courierID, reference string, amountMinor int64, adminID string) error {
	if amountMinor <= 0 || courierID == "" || reference == "" {
		return ledgerdomain.ErrInvalidEntry
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin courier remittance: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var alreadyRecorded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ledger_entries WHERE source_key=$1)`, "courier-remittance:"+reference).Scan(&alreadyRecorded); err != nil {
		return fmt.Errorf("check courier remittance idempotency: %w", err)
	}
	if alreadyRecorded {
		var matches bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ledger_entries le JOIN ledger_lines ll ON ll.entry_id=le.id JOIN ledger_accounts la ON la.id=ll.account_id WHERE le.source_key=$1 AND la.account_key=$2 AND ll.amount_minor=$3)`, "courier-remittance:"+reference, ledgerdomain.AccountKey(ledgerdomain.CourierCashReceivable, courierID), -amountMinor).Scan(&matches); err != nil {
			return fmt.Errorf("verify courier remittance replay: %w", err)
		}
		if !matches {
			return ledgerdomain.ErrInvalidEntry
		}
		return tx.Commit(ctx)
	}
	var balance int64
	err = tx.QueryRow(ctx, `SELECT balance_minor FROM courier_cash_balances WHERE courier_id=$1 FOR UPDATE`, courierID).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerdomain.ErrInsufficientCash
	}
	if err != nil {
		return fmt.Errorf("lock courier cash balance: %w", err)
	}
	if _, err := ledgerdomain.RemittanceBalance(balance, amountMinor); err != nil {
		return err
	}
	entry := ledgerdomain.Entry{
		SourceKey: "courier-remittance:" + reference, EntryType: "courier_remittance", Currency: "ETB", Description: "Courier cash remittance",
		Lines: []ledgerdomain.Line{
			{AccountKey: ledgerdomain.AccountKey(ledgerdomain.SystemClearing, ""), AccountType: ledgerdomain.SystemClearing, AmountMinor: amountMinor},
			{AccountKey: ledgerdomain.AccountKey(ledgerdomain.CourierCashReceivable, courierID), AccountType: ledgerdomain.CourierCashReceivable, OwnerUserID: courierID, AmountMinor: -amountMinor},
		},
	}
	created, err := recordTx(ctx, pgxTransaction{tx}, entry)
	if err != nil {
		return err
	}
	if !created {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE courier_cash_balances SET balance_minor=balance_minor-$2,updated_at=now() WHERE courier_id=$1`, courierID, amountMinor); err != nil {
		return fmt.Errorf("reduce courier cash balance: %w", err)
	}
	if adminID != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO admin_audit_log(admin_user_id,action,entity_type,entity_id,details) VALUES($1,'courier.settle_cod','courier',$2,jsonb_build_object('reference',$3,'amount_minor',$4))`, adminID, courierID, reference, amountMinor); err != nil {
			return fmt.Errorf("audit courier remittance: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit courier remittance: %w", err)
	}
	return nil
}

// RecordRestaurantPayout pays no more than the restaurant's current payable
// credit balance. The unique source key makes retries safe.
func (r *Repository) RecordRestaurantPayout(ctx context.Context, restaurantID, reference string, amountMinor int64) error {
	if amountMinor <= 0 || restaurantID == "" || reference == "" {
		return ledgerdomain.ErrInvalidEntry
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin restaurant payout: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var alreadyRecorded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ledger_entries WHERE source_key=$1)`, "restaurant-payout:"+reference).Scan(&alreadyRecorded); err != nil {
		return fmt.Errorf("check restaurant payout idempotency: %w", err)
	}
	if alreadyRecorded {
		return tx.Commit(ctx)
	}
	accountKey := ledgerdomain.AccountKey(ledgerdomain.RestaurantPayable, restaurantID)
	var accountID string
	err = tx.QueryRow(ctx, `INSERT INTO ledger_accounts(account_key,account_type,owner_restaurant_id)
VALUES($1,'restaurant_payable',$2) ON CONFLICT(account_key) DO UPDATE SET account_key=EXCLUDED.account_key
RETURNING id::text`, accountKey, restaurantID).Scan(&accountID)
	if err != nil {
		return fmt.Errorf("resolve restaurant payable account: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT id::text FROM ledger_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&accountID); err != nil {
		return fmt.Errorf("lock restaurant payable account: %w", err)
	}
	var signedBalance int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(ll.amount_minor),0) FROM ledger_lines ll WHERE ll.account_id=$1`, accountID).Scan(&signedBalance); err != nil {
		return fmt.Errorf("load restaurant payable balance: %w", err)
	}
	if signedBalance >= 0 || amountMinor > -signedBalance {
		return ledgerdomain.ErrInsufficientPayable
	}
	entry := ledgerdomain.Entry{
		SourceKey: "restaurant-payout:" + reference, EntryType: "restaurant_payout", Currency: "ETB", Description: "Restaurant partner payout",
		Lines: []ledgerdomain.Line{
			{AccountKey: accountKey, AccountType: ledgerdomain.RestaurantPayable, OwnerRestaurantID: restaurantID, AmountMinor: amountMinor},
			{AccountKey: ledgerdomain.AccountKey(ledgerdomain.SystemClearing, ""), AccountType: ledgerdomain.SystemClearing, AmountMinor: -amountMinor},
		},
	}
	if _, err := recordTx(ctx, pgxTransaction{tx}, entry); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit restaurant payout: %w", err)
	}
	return nil
}
