//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pgadapter "github.com/mesob-delivery/backend/internal/adapter/postgres"
	orderspostgres "github.com/mesob-delivery/backend/internal/orders/adapter/postgres"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestKitchenRejectAndSLATimeoutRestoreInventory(t *testing.T) {
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
	databaseURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/mesob?sslmode=disable", host, port.Port())
	migrations, err := filepath.Abs("../../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrationsReady(ctx, databaseURL, migrations); err != nil {
		t.Fatal(err)
	}
	pool, err := pgadapter.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var owner, customer, restaurant, address, category, item string
	if err = pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES('Kitchen Owner','+251900000071','restaurant_owner','active') RETURNING id::text`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES('Kitchen Customer','+251900000072','customer','active') RETURNING id::text`).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'Kitchen Test',9,38.75,5,true,10,'approved') RETURNING id::text`, owner).Scan(&restaurant); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO addresses(user_id,label,lat,lng,line1) VALUES($1,'Home',9,38.75,'Test') RETURNING id::text`, customer).Scan(&address); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO menu_categories(restaurant_id,name) VALUES($1,'Meals') RETURNING id::text`, restaurant).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO menu_items(category_id,name,price_minor,stock_limit,stock_remaining) VALUES($1,'Pasta',10000,5,0) RETURNING id::text`, category).Scan(&item); err != nil {
		t.Fatal(err)
	}
	makeOrder := func(status string) string {
		var id string
		if e := pool.QueryRow(ctx, `INSERT INTO orders(user_id,restaurant_id,delivery_address_id,status,payment_status,payment_method,subtotal_minor,delivery_fee_minor,commission_minor,total_minor,currency,placed_at) VALUES($1,$2,$3,$4,'paid','CHAPA',10000,15000,1000,25000,'ETB',now()) RETURNING id::text`, customer, restaurant, address, status).Scan(&id); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `INSERT INTO order_items(order_id,menu_item_id,item_name,unit_price_minor,quantity,line_total_minor) VALUES($1,$2,'Pasta',10000,2,20000)`, id, item); e != nil {
			t.Fatal(e)
		}
		return id
	}
	rejected := makeOrder("PAID")
	timeout := makeOrder("PAID")
	repo := orderspostgres.NewRepository(pool)
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='READY_FOR_PICKUP' WHERE id=$1`, rejected); err == nil {
		t.Fatal("database accepted invalid PAID -> READY_FOR_PICKUP transition")
	}
	if err := repo.RejectRestaurantOrder(ctx, "00000000-0000-0000-0000-000000000001", rejected, domain.RejectOutOfStock); err == nil {
		t.Fatal("restaurant without ownership rejected another restaurant order")
	}
	if err := repo.RejectRestaurantOrder(ctx, owner, rejected, domain.RejectOutOfStock); err != nil {
		t.Fatal(err)
	}
	if err := repo.TimeoutRestaurantOrder(ctx, timeout); err != nil {
		t.Fatal(err)
	}
	var stock int
	var rejectedStatus, rejectedPayment, timeoutStatus string
	if err := pool.QueryRow(ctx, `SELECT stock_remaining FROM menu_items WHERE id=$1`, item).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status,payment_status FROM orders WHERE id=$1`, rejected).Scan(&rejectedStatus, &rejectedPayment); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, timeout).Scan(&timeoutStatus); err != nil {
		t.Fatal(err)
	}
	if stock != 4 || rejectedStatus != "REJECTED_BY_RESTAURANT" || rejectedPayment != "refund_pending" || timeoutStatus != "CANCELLED_TIMEOUT" {
		t.Fatalf("stock=%d reject=%s/%s timeout=%s", stock, rejectedStatus, rejectedPayment, timeoutStatus)
	}
}
