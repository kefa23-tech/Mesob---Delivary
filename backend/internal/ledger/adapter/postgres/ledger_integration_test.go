//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	pgadapter "github.com/mesob-delivery/backend/internal/adapter/postgres"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresRejectsUnbalancedLedgerEntryAtCommit(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{
		Image: "postgis/postgis:16-3.4", ExposedPorts: []string{"5432/tcp"},
		Env:        map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "mesob"},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(3 * time.Minute),
	}, Started: true})
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
	databaseURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/mesob?sslmode=disable", host, port.Port())
	migrations, err := filepath.Abs("../../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := waitLedgerMigrations(ctx, databaseURL, migrations); err != nil {
		t.Fatal(err)
	}
	pool, err := pgadapter.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var debitAccountID, creditAccountID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM ledger_accounts WHERE account_key='system_clearing'`).Scan(&debitAccountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM ledger_accounts WHERE account_key='customer_clearing'`).Scan(&creditAccountID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var entryID string
	if err := tx.QueryRow(ctx, `INSERT INTO ledger_entries(source_key,entry_type,description) VALUES('test:unbalanced','test','unbalanced') RETURNING id::text`).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_lines(entry_id,line_number,account_id,amount_minor) VALUES($1,1,$2,100),($1,2,$3,-99)`, entryID, debitAccountID, creditAccountID); err != nil {
		t.Fatal(err)
	}
	err = tx.Commit(ctx)
	if err == nil || !strings.Contains(err.Error(), "not balanced") {
		t.Fatalf("expected deferred database balance constraint to reject commit, got %v", err)
	}

	var ownerID, customerID, courierID, restaurantID, addressID, orderID string
	for _, user := range []struct {
		name, phone, role string
		into              *string
	}{{"Owner", "+251900000201", "restaurant_owner", &ownerID}, {"Customer", "+251900000202", "customer", &customerID}, {"Courier", "+251900000203", "courier", &courierID}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES($1,$2,$3,'active') RETURNING id::text`, user.name, user.phone, user.role).Scan(user.into); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'Ledger Restaurant',9,38.75,5,true,10,'approved') RETURNING id::text`, ownerID).Scan(&restaurantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO addresses(user_id,label,lat,lng,line1) VALUES($1,'Home',9,38.75,'Test street') RETURNING id::text`, customerID).Scan(&addressID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,restaurant_id,delivery_address_id,courier_id,status,payment_status,payment_method,subtotal_minor,commission_minor,delivery_fee_minor,total_minor,currency)
VALUES($1,$2,$3,$4,'pending','pending','COD',100,10,15,115,'ETB') RETURNING id::text`, customerID, restaurantID, addressID, courierID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='delivered' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	var lineCount int
	var signedTotal, cashBalance int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(ll.amount_minor) FROM ledger_entries le JOIN ledger_lines ll ON ll.entry_id=le.id WHERE le.source_key=$1`, "order:"+orderID+":delivered").Scan(&lineCount, &signedTotal); err != nil {
		t.Fatal(err)
	}
	if lineCount != 4 || signedTotal != 0 {
		t.Fatalf("COD delivery journal lines=%d signed total=%d; want 4 and 0", lineCount, signedTotal)
	}
	if err := pool.QueryRow(ctx, `SELECT balance_minor FROM courier_cash_balances WHERE courier_id=$1`, courierID).Scan(&cashBalance); err != nil {
		t.Fatal(err)
	}
	if cashBalance != 115 {
		t.Fatalf("courier cash balance=%d; want 115", cashBalance)
	}
	var payableAccountID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM ledger_accounts WHERE account_key=$1`, "restaurant_payable:"+restaurantID).Scan(&payableAccountID); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO ledger_lines(entry_id,line_number,account_id,amount_minor) VALUES
((SELECT id FROM ledger_entries WHERE source_key=$1),5,$2,1),
((SELECT id FROM ledger_entries WHERE source_key=$1),6,$3,-1)`, "order:"+orderID+":delivered", debitAccountID, payableAccountID)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected committed journal entry to reject appended lines, got %v", err)
	}
}

func waitLedgerMigrations(ctx context.Context, databaseURL, migrations string) error {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		lastErr = pgadapter.RunMigrations(ctx, databaseURL, migrations)
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return lastErr
}
