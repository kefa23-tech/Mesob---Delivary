//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pgadapter "github.com/mesob-delivery/backend/internal/adapter/postgres"
	orderspostgres "github.com/mesob-delivery/backend/internal/orders/adapter/postgres"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestConcurrentCheckoutCannotOversellLastItem(t *testing.T) {
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
	if err := migrationsReady(ctx, databaseURL, migrations); err != nil {
		t.Fatal(err)
	}
	pool, err := pgadapter.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var ownerID, userID, restaurantID, addressID, categoryID, itemID string
	if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES('Owner','+251900000091','restaurant_owner','active') RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES('Customer','+251900000092','customer','active') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'Checkout Restaurant',9,38.75,5,true,10,'approved') RETURNING id::text`, ownerID).Scan(&restaurantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO addresses(user_id,label,lat,lng,line1) VALUES($1,'Home',9,38.75,'Test street') RETURNING id::text`, userID).Scan(&addressID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_categories(restaurant_id,name) VALUES($1,'Mains') RETURNING id::text`, restaurantID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items(category_id,name,price_minor,stock_limit,stock_remaining,is_available) VALUES($1,'Last Injera',10000,1,1,true) RETURNING id::text`, categoryID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	repo := orderspostgres.NewRepository(pool)
	request := domain.CheckoutRequest{AddressID: addressID, PaymentMethod: domain.PaymentChapa, Items: []domain.CheckoutItem{{MenuItemID: itemID, Quantity: 1}}}
	start := make(chan struct{})
	type result struct {
		order domain.Order
		err   error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, key := range []string{"stock-race-a", "stock-race-b"} {
		workers.Add(1)
		go func(key string) {
			defer workers.Done()
			<-start
			order, err := repo.Place(ctx, userID, key, request, nil)
			results <- result{order, err}
		}(key)
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, soldOut := 0, 0
	for result := range results {
		if result.err == nil {
			succeeded++
			continue
		}
		if errors.Is(result.err, domain.ErrInsufficientStock) {
			soldOut++
			continue
		}
		t.Fatalf("unexpected checkout result: %v", result.err)
	}
	if succeeded != 1 || soldOut != 1 {
		t.Fatalf("successes=%d sold-out failures=%d; want one each", succeeded, soldOut)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT stock_remaining FROM menu_items WHERE id=$1`, itemID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("remaining stock=%d; want 0", remaining)
	}
	var orderCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE user_id=$1`, userID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Fatalf("orders=%d; want exactly one", orderCount)
	}
}

func migrationsReady(ctx context.Context, databaseURL, migrations string) error {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		lastErr = pgadapter.RunMigrations(ctx, databaseURL, migrations)
		if lastErr == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return lastErr
}
