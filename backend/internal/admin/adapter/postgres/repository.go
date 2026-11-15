package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/admin/domain"
	ledgerpg "github.com/mesob-delivery/backend/internal/ledger/adapter/postgres"
	ledgerdomain "github.com/mesob-delivery/backend/internal/ledger/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Overview(ctx context.Context) (domain.Overview, error) {
	var out domain.Overview
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(sum(total_minor) FILTER (WHERE payment_status='paid' AND COALESCE(placed_at,created_at)>=date_trunc('day',now())),0),count(*) FILTER (WHERE status NOT IN ('DELIVERED','delivered','cancelled','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT')) FROM orders`).Scan(&out.DailyGMVMinor, &out.ActiveOrders)
	if err != nil {
		return out, err
	}
	var stuck int
	err = r.pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE courier_id IS NULL AND ((status IN ('PAID','CONFIRMED_COD') AND COALESCE(placed_at,created_at)<now()-interval '20 minutes') OR EXISTS(SELECT 1 FROM order_events e WHERE e.order_id=orders.id AND e.event_type='dispatch.manual_intervention'))`).Scan(&stuck)
	out.StuckOrders = stuck
	return out, err
}

func (r *Repository) PendingKYC(ctx context.Context) (domain.KYCQueue, error) {
	out := domain.KYCQueue{Restaurants: []domain.RestaurantKYC{}, Couriers: []domain.CourierKYC{}}
	rows, err := r.pool.Query(ctx, `SELECT r.id::text,r.name,u.name,COALESCE(r.tin,''),COALESCE(r.business_license_url,''),r.approval_status,r.created_at::text FROM restaurants r JOIN users u ON u.id=r.owner_id WHERE r.approval_status='pending' ORDER BY r.created_at`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var x domain.RestaurantKYC
		if err := rows.Scan(&x.ID, &x.Name, &x.OwnerName, &x.TIN, &x.BusinessLicenseURL, &x.Status, &x.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Restaurants = append(out.Restaurants, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT id::text,name,phone,COALESCE(national_id,''),COALESCE(driver_license_url,''),courier_vehicle_info,status,created_at::text FROM users WHERE role='courier' AND status='pending' ORDER BY created_at`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var x domain.CourierKYC
		var vehicle []byte
		if err := rows.Scan(&x.ID, &x.Name, &x.Phone, &x.NationalID, &x.DriverLicenseURL, &vehicle, &x.Status, &x.CreatedAt); err != nil {
			return out, err
		}
		_ = json.Unmarshal(vehicle, &x.VehicleInfo)
		if x.VehicleInfo == nil {
			x.VehicleInfo = map[string]any{}
		}
		out.Couriers = append(out.Couriers, x)
	}
	return out, rows.Err()
}

func (r *Repository) ReviewKYC(ctx context.Context, req domain.ReviewRequest, adminID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var tag pgconnTag
	if req.EntityType == "restaurant" {
		tag, err = tx.Exec(ctx, `UPDATE restaurants SET approval_status=$2,kyc_review_notes=$3,kyc_reviewed_at=now(),updated_at=now() WHERE id=$1 AND approval_status='pending'`, req.ID, req.Decision, req.Notes)
	} else {
		status := "suspended"
		if req.Decision == "approved" {
			status = "active"
		}
		tag, err = tx.Exec(ctx, `UPDATE users SET status=$2,kyc_review_notes=$3,kyc_reviewed_at=now(),updated_at=now() WHERE id=$1 AND role='courier' AND status='pending'`, req.ID, status, req.Notes)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if err = audit(ctx, tx, adminID, "kyc."+req.Decision, req.EntityType, req.ID, map[string]string{"notes": req.Notes}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// pgconnTag keeps the service layer independent of driver-specific command tags.
type pgconnTag interface{ RowsAffected() int64 }

func (r *Repository) StuckOrders(ctx context.Context, minutes int) ([]domain.StuckOrder, error) {
	rows, err := r.pool.Query(ctx, `SELECT o.id::text,o.restaurant_id::text,r.name,c.name,o.status,o.payment_method,o.total_minor,o.placed_at::text,GREATEST(0,floor(extract(epoch FROM (now()-COALESCE(o.placed_at,o.created_at)))/60)::int),EXISTS(SELECT 1 FROM order_events e WHERE e.order_id=o.id AND e.event_type='dispatch.manual_intervention'),COALESCE((SELECT jsonb_agg(jsonb_build_object('id',u.id::text,'name',u.name,'vehicle_type',cp.vehicle_type) ORDER BY u.name) FROM users u JOIN courier_presence cp ON cp.courier_id=u.id AND cp.online=true LEFT JOIN courier_cash_balances b ON b.courier_id=u.id WHERE u.role='courier' AND u.status='active' AND NOT EXISTS(SELECT 1 FROM orders active WHERE active.courier_id=u.id AND active.status IN ('COURIER_ASSIGNED','PICKED_UP','dispatched')) AND (o.payment_method<>'COD' OR COALESCE(b.balance_minor,0)+COALESCE(b.reserved_minor,0)+o.total_minor<=u.courier_cod_limit_minor)),'[]'::jsonb)
FROM orders o JOIN restaurants r ON r.id=o.restaurant_id JOIN users c ON c.id=o.user_id
WHERE o.courier_id IS NULL AND ((o.status IN ('PAID','CONFIRMED_COD') AND COALESCE(o.placed_at,o.created_at)<now()-make_interval(mins=>$1)) OR EXISTS(SELECT 1 FROM order_events e WHERE e.order_id=o.id AND e.event_type='dispatch.manual_intervention'))
ORDER BY o.placed_at NULLS FIRST LIMIT 200`, minutes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.StuckOrder{}
	for rows.Next() {
		var x domain.StuckOrder
		var couriers []byte
		if err := rows.Scan(&x.ID, &x.RestaurantID, &x.RestaurantName, &x.CustomerName, &x.Status, &x.PaymentMethod, &x.TotalMinor, &x.PlacedAt, &x.MinutesStuck, &x.ManualIntervention, &couriers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(couriers, &x.EligibleCouriers); err != nil {
			return nil, err
		}
		if x.EligibleCouriers == nil {
			x.EligibleCouriers = []domain.EligibleCourier{}
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *Repository) ManualAssign(ctx context.Context, req domain.AssignmentRequest, adminID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1 FOR UPDATE`, req.OrderID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "ACCEPTED" && status != "PREPARING" && status != "READY_FOR_PICKUP" {
		return domain.ErrConflict
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN courier_presence cp ON cp.courier_id=u.id AND cp.online=true WHERE u.id=$1 AND u.role='courier' AND u.status='active' AND NOT EXISTS(SELECT 1 FROM orders a WHERE a.courier_id=u.id AND a.status IN ('COURIER_ASSIGNED','PICKED_UP','dispatched')))`, req.CourierID).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return domain.ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE orders SET courier_id=$2,status='COURIER_ASSIGNED',courier_assigned_at=now(),pickup_code=COALESCE(pickup_code,lpad((floor(random()*1000000))::bigint::text,6,'0')),updated_at=now() WHERE id=$1 AND courier_id IS NULL`, req.OrderID, req.CourierID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return domain.ErrConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'dispatch.manual_assignment',jsonb_build_object('courier_id',$2,'admin_id',$3)) ON CONFLICT(order_id,event_type) DO UPDATE SET payload=EXCLUDED.payload`, req.OrderID, req.CourierID, adminID)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, adminID, "order.manual_assign", "order", req.OrderID, map[string]string{"courier_id": req.CourierID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ResolveDispute(ctx context.Context, req domain.DisputeRequest, adminID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var orderStatus, paymentStatus, currency string
	var total int64
	var paymentMethod string
	err = tx.QueryRow(ctx, `SELECT status,payment_status,payment_method,currency,total_minor FROM orders WHERE id=$1 FOR UPDATE`, req.OrderID).Scan(&orderStatus, &paymentStatus, &paymentMethod, &currency, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if orderStatus == "DELIVERED" || orderStatus == "delivered" {
		return domain.ErrConflict
	}
	refund := req.RefundMinor
	if req.Resolution == "full_refund" || req.Resolution == "cancellation" {
		refund = total
	}
	if req.Resolution == "partial_credit" && refund == 0 {
		return domain.ErrInvalidRequest
	}
	if refund > total {
		return domain.ErrInvalidRequest
	}
	if refund > 0 && (paymentStatus != "paid" || paymentMethod == "COD") && req.Resolution != "cancellation" {
		return domain.ErrConflict
	}
	if refund > 0 {
		var refunded int64
		if err = tx.QueryRow(ctx, `SELECT COALESCE(-sum(ll.amount_minor),0)::bigint FROM ledger_entries le JOIN ledger_lines ll ON ll.entry_id=le.id JOIN ledger_accounts la ON la.id=ll.account_id WHERE le.order_id=$1 AND le.entry_type='admin_refund' AND la.account_type='customer_clearing'`, req.OrderID).Scan(&refunded); err != nil {
			return err
		}
		if refunded+refund > total {
			return domain.ErrConflict
		}
	}
	var disputeID string
	if err = tx.QueryRow(ctx, `INSERT INTO order_disputes(order_id,reason,status,resolution,reviewer_notes,resolved_by,resolved_at) VALUES($1,'Admin resolution','resolved',$2,$3,$4,now()) RETURNING id::text`, req.OrderID, req.Resolution, req.Notes, adminID).Scan(&disputeID); err != nil {
		return err
	}
	if refund > 0 && paymentStatus == "paid" && paymentMethod != "COD" {
		if err = recordRefund(ctx, tx, req.OrderID, disputeID, currency, refund); err != nil {
			return err
		}
	}
	if req.Resolution == "full_refund" && paymentStatus == "paid" && paymentMethod != "COD" {
		if _, err = tx.Exec(ctx, `UPDATE orders SET payment_status='refunded',updated_at=now() WHERE id=$1`, req.OrderID); err != nil {
			return err
		}
	}
	if req.Resolution == "cancellation" {
		if _, err = tx.Exec(ctx, `SET LOCAL app.admin_operation='true'`); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE orders SET status='cancelled',payment_status=CASE WHEN payment_status='paid' THEN 'refunded' ELSE payment_status END,updated_at=now() WHERE id=$1`, req.OrderID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE menu_items mi SET stock_remaining=LEAST(mi.stock_limit,COALESCE(mi.stock_remaining,0)+oi.quantity),updated_at=now() FROM order_items oi WHERE oi.order_id=$1 AND oi.menu_item_id=mi.id AND mi.stock_limit IS NOT NULL`, req.OrderID); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, adminID, "order.resolve_dispute", "order", req.OrderID, map[string]any{"resolution": req.Resolution, "refund_minor": refund, "notes": req.Notes, "dispute_id": disputeID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recordRefund(ctx context.Context, tx pgx.Tx, orderID, disputeID, currency string, amount int64) error {
	rows, err := tx.Query(ctx, `SELECT la.account_key,la.account_type,COALESCE(la.owner_user_id::text,''),COALESCE(la.owner_restaurant_id::text,''),ll.amount_minor+COALESCE((SELECT sum(prior.amount_minor) FROM ledger_entries prev JOIN ledger_lines prior ON prior.entry_id=prev.id WHERE prev.order_id=le.order_id AND prev.entry_type='admin_refund' AND prior.account_id=ll.account_id AND prior.amount_minor>0),0) FROM ledger_entries le JOIN ledger_lines ll ON ll.entry_id=le.id JOIN ledger_accounts la ON la.id=ll.account_id WHERE le.order_id=$1 AND le.entry_type='online_payment' AND ll.amount_minor<0 AND la.account_type<>'customer_clearing' ORDER BY ll.line_number`, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	sourceCredits := []ledgerdomain.Line{}
	for rows.Next() {
		var key, kind, userID, restaurantID string
		var signed int64
		if err := rows.Scan(&key, &kind, &userID, &restaurantID, &signed); err != nil {
			return err
		}
		sourceCredits = append(sourceCredits, ledgerdomain.Line{AccountKey: key, AccountType: ledgerdomain.AccountType(kind), OwnerUserID: userID, OwnerRestaurantID: restaurantID, AmountMinor: signed})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	e, err := ledgerdomain.RefundEntry("admin-dispute:"+disputeID, orderID, currency, "Admin dispute refund", amount, sourceCredits)
	if err != nil {
		return err
	}
	return ledgerpg.RecordWithinTransaction(ctx, tx, e)
}

func (r *Repository) LedgerSummary(ctx context.Context) (domain.LedgerSummary, error) {
	out := domain.LedgerSummary{Accounts: map[string]int64{}}
	rows, err := r.pool.Query(ctx, `SELECT la.account_type,COALESCE(sum(ll.amount_minor),0)::bigint FROM ledger_accounts la LEFT JOIN ledger_lines ll ON ll.account_id=la.id GROUP BY la.account_type`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return out, err
		}
		out.Accounts[k] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor),0)::bigint FROM ledger_lines`).Scan(&out.TotalMinor); err != nil {
		return out, err
	}
	out.Balanced = out.TotalMinor == 0
	return out, nil
}

func (r *Repository) LedgerEntries(ctx context.Context, page, limit int, accountType, query string) (domain.LedgerPage, error) {
	out := domain.LedgerPage{Data: []domain.LedgerEntry{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT count(DISTINCT le.id) FROM ledger_entries le LEFT JOIN ledger_lines ll ON ll.entry_id=le.id LEFT JOIN ledger_accounts la ON la.id=ll.account_id WHERE ($1='' OR la.account_type=$1) AND ($2='' OR le.source_key ILIKE '%'||$2||'%' OR le.description ILIKE '%'||$2||'%' OR le.order_id::text ILIKE '%'||$2||'%')`, accountType, query).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT le.id::text,le.source_key,COALESCE(le.order_id::text,''),le.entry_type,le.currency,le.description,le.created_at::text FROM ledger_entries le LEFT JOIN ledger_lines ll ON ll.entry_id=le.id LEFT JOIN ledger_accounts la ON la.id=ll.account_id WHERE ($1='' OR la.account_type=$1) AND ($2='' OR le.source_key ILIKE '%'||$2||'%' OR le.description ILIKE '%'||$2||'%' OR le.order_id::text ILIKE '%'||$2||'%') ORDER BY le.created_at DESC LIMIT $3 OFFSET $4`, accountType, query, limit, (page-1)*limit)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var x domain.LedgerEntry
		if err := rows.Scan(&x.ID, &x.SourceKey, &x.OrderID, &x.EntryType, &x.Currency, &x.Description, &x.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		x.Lines = []domain.LedgerLine{}
		out.Data = append(out.Data, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	for i := range out.Data {
		lines, err := r.pool.Query(ctx, `SELECT la.account_key,la.account_type,ll.amount_minor FROM ledger_lines ll JOIN ledger_accounts la ON la.id=ll.account_id WHERE ll.entry_id=$1 ORDER BY ll.line_number`, out.Data[i].ID)
		if err != nil {
			return out, err
		}
		for lines.Next() {
			var l domain.LedgerLine
			if err := lines.Scan(&l.AccountKey, &l.AccountType, &l.AmountMinor); err != nil {
				lines.Close()
				return out, err
			}
			out.Data[i].Lines = append(out.Data[i].Lines, l)
		}
		if err := lines.Err(); err != nil {
			lines.Close()
			return out, err
		}
		lines.Close()
	}
	return out, nil
}

func (r *Repository) SettleCOD(ctx context.Context, req domain.SettlementRequest, adminID string) error {
	if req.AmountMinor <= 0 || strings.TrimSpace(req.Reference) == "" {
		return domain.ErrInvalidRequest
	}
	err := ledgerpg.NewRepository(r.pool).RecordCourierRemittanceForAdmin(ctx, req.CourierID, req.Reference, req.AmountMinor, adminID)
	if errors.Is(err, ledgerdomain.ErrInsufficientCash) {
		return domain.ErrConflict
	}
	return err
}

func audit(ctx context.Context, tx pgx.Tx, adminID, action, entityType, entityID string, details any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_log(admin_user_id,action,entity_type,entity_id,details) VALUES($1,$2,$3,$4,$5::jsonb)`, adminID, action, entityType, entityID, string(b))
	return err
}
