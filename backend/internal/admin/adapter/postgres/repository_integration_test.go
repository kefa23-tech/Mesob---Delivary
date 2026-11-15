//go:build integration

package postgres

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pgadapter "github.com/mesob-delivery/backend/internal/adapter/postgres"
	"github.com/mesob-delivery/backend/internal/admin/domain"
	ledgerpg "github.com/mesob-delivery/backend/internal/ledger/adapter/postgres"
	ledgerdomain "github.com/mesob-delivery/backend/internal/ledger/domain"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestAdminDisputeReversalAndCODSettlement(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "postgis/postgis:16-3.4", ExposedPorts: []string{"5432/tcp"}, Env: map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "mesob"}, WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(3 * time.Minute)}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	defer container.Terminate(ctx)
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("postgres://postgres:postgres@%s:%s/mesob?sslmode=disable", host, port.Port())
	migrations, err := filepath.Abs("../../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	var migrationErr error
	for attempt := 0; attempt < 30; attempt++ {
		migrationErr = pgadapter.RunMigrations(ctx, url, migrations)
		if migrationErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
	if migrationErr != nil {
		t.Fatal(migrationErr)
	}
	pool, err := pgadapter.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var adminID, ownerID, courierID, customerID, restaurantID, addressID, orderID string
	for _, u := range []struct {
		name, phone, role string
		id                *string
	}{{"Admin", "+251900001001", "admin", &adminID}, {"Owner", "+251900001002", "restaurant_owner", &ownerID}, {"Courier", "+251900001003", "courier", &courierID}, {"Customer", "+251900001004", "customer", &customerID}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES($1,$2,$3,'active') RETURNING id::text`, u.name, u.phone, u.role).Scan(u.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'Ops Test',9,38.75,5,true,10,'approved') RETURNING id::text`, ownerID).Scan(&restaurantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO addresses(user_id,label,lat,lng,line1) VALUES($1,'Home',9,38.75,'Test street') RETURNING id::text`, customerID).Scan(&addressID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,restaurant_id,delivery_address_id,status,payment_status,payment_method,subtotal_minor,delivery_fee_minor,commission_minor,total_minor,currency) VALUES($1,$2,$3,'PAID','paid','CHAPA',10000,1000,1000,11000,'ETB') RETURNING id::text`, customerID, restaurantID, addressID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry := ledgerdomain.OnlinePaymentEntry(orderID, "chapa", "integration-reference", restaurantID, 11000, 10000, 1000, 1000)
	if err := ledgerpg.RecordWithinTransaction(ctx, tx, entry); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	admin := NewRepository(pool)
	if _, err := admin.StuckOrders(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if err := admin.ResolveDispute(ctx, domain.DisputeRequest{OrderID: orderID, Resolution: "partial_credit", RefundMinor: 3000, Notes: "Partial service recovery"}, adminID); err != nil {
		t.Fatal(err)
	}
	var refundLines int
	var refundTotal int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(ll.amount_minor) FROM ledger_entries le JOIN ledger_lines ll ON ll.entry_id=le.id WHERE le.order_id=$1 AND le.entry_type='admin_refund'`, orderID).Scan(&refundLines, &refundTotal); err != nil {
		t.Fatal(err)
	}
	if refundLines < 2 || refundTotal != 0 {
		t.Fatalf("refund lines=%d sum=%d; want at least 2 lines and zero sum", refundLines, refundTotal)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO courier_cash_balances(courier_id,balance_minor,reserved_minor,currency) VALUES($1,50000,0,'ETB')`, courierID); err != nil {
		t.Fatal(err)
	}
	if err := admin.SettleCOD(ctx, domain.SettlementRequest{CourierID: courierID, Reference: "integration-cod-slip", AmountMinor: 12000}, adminID); err != nil {
		t.Fatal(err)
	}
	if err := admin.SettleCOD(ctx, domain.SettlementRequest{CourierID: courierID, Reference: "integration-cod-slip", AmountMinor: 12000}, adminID); err != nil {
		t.Fatalf("idempotent remittance replay failed: %v", err)
	}
	var remaining int64
	if err := pool.QueryRow(ctx, `SELECT balance_minor FROM courier_cash_balances WHERE courier_id=$1`, courierID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 38000 {
		t.Fatalf("courier cash exposure=%d; want 38000", remaining)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE admin_user_id=$1`, adminID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("admin audit entries=%d; want 2", auditCount)
	}
	var signed int64
	if err := pool.QueryRow(ctx, `SELECT sum(amount_minor) FROM ledger_lines`).Scan(&signed); err != nil {
		t.Fatal(err)
	}
	if signed != 0 {
		t.Fatalf("global signed ledger total=%d; want 0", signed)
	}
}
