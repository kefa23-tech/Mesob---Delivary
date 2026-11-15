//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pgadapter "github.com/mesob-delivery/backend/internal/adapter/postgres"
	dispatchpostgres "github.com/mesob-delivery/backend/internal/dispatch/adapter/postgres"
	dispatchdomain "github.com/mesob-delivery/backend/internal/dispatch/domain"
	orderspostgres "github.com/mesob-delivery/backend/internal/orders/adapter/postgres"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestCODCheckoutEnforcesLifetimeEligibilityAndOrderCap(t *testing.T) {
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
	var ownerID, userID, newUserID, courierID, restaurantID, addressID, newAddressID, categoryID, atCapItemID, overCapItemID string
	for _, user := range []struct {
		name, phone string
		into        *string
	}{{"Owner", "+251900000101", &ownerID}, {"Experienced", "+251900000102", &userID}, {"New", "+251900000103", &newUserID}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES($1,$2,$3,'active') RETURNING id::text`, user.name, user.phone, map[bool]string{true: "restaurant_owner", false: "customer"}[user.into == &ownerID]).Scan(user.into); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'COD Restaurant',9,38.75,5,true,10,'approved') RETURNING id::text`, ownerID).Scan(&restaurantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES('Courier','+251900000104','courier','active') RETURNING id::text`).Scan(&courierID); err != nil {
		t.Fatal(err)
	}
	for _, address := range []struct {
		userID string
		into   *string
	}{{userID, &addressID}, {newUserID, &newAddressID}} {
		if err := pool.QueryRow(ctx, `INSERT INTO addresses(user_id,label,lat,lng,line1) VALUES($1,'Home',9,38.75,'Test street') RETURNING id::text`, address.userID).Scan(address.into); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_categories(restaurant_id,name) VALUES($1,'Mains') RETURNING id::text`, restaurantID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items(category_id,name,price_minor,is_available) VALUES($1,'At COD cap',185000,true) RETURNING id::text`, categoryID).Scan(&atCapItemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items(category_id,name,price_minor,is_available) VALUES($1,'Above COD cap',185001,true) RETURNING id::text`, categoryID).Scan(&overCapItemID); err != nil {
		t.Fatal(err)
	}
	// A prior delivered order satisfies the new-account rule.
	if _, err := pool.Exec(ctx, `INSERT INTO orders(user_id,restaurant_id,delivery_address_id,status,payment_status,payment_method,total_minor) VALUES($1,$2,$3,'delivered','paid','COD',10000)`, userID, restaurantID, addressID); err != nil {
		t.Fatal(err)
	}
	repo := orderspostgres.NewRepository(pool)
	atCapOrder, err := repo.Place(ctx, userID, "cod-at-cap", domain.CheckoutRequest{AddressID: addressID, PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: atCapItemID, Quantity: 1}}}, nil)
	if err != nil {
		t.Fatalf("COD order totaling exactly 200000 santim rejected: %v", err)
	}
	if _, err := repo.Place(ctx, userID, "cod-over-cap", domain.CheckoutRequest{AddressID: addressID, PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: overCapItemID, Quantity: 1}}}, nil); !errors.Is(err, domain.ErrCODOrderLimit) {
		t.Fatalf("expected COD cap error, got %v", err)
	}
	if _, err := repo.Place(ctx, newUserID, "cod-first-order", domain.CheckoutRequest{AddressID: newAddressID, PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: atCapItemID, Quantity: 1}}}, nil); !errors.Is(err, domain.ErrCODFirstOrder) {
		t.Fatalf("expected first-order COD rejection, got %v", err)
	}
	assignmentRepo := dispatchpostgres.NewRepository(pool)
	if err := assignmentRepo.AssignCourier(ctx, atCapOrder.ID, courierID); err != nil {
		t.Fatalf("first COD courier assignment rejected: %v", err)
	}
	secondOrder, err := repo.Place(ctx, userID, "cod-at-cap-second", domain.CheckoutRequest{AddressID: addressID, PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: atCapItemID, Quantity: 1}}}, nil)
	if err != nil {
		t.Fatalf("second COD checkout rejected: %v", err)
	}
	if err := assignmentRepo.AssignCourier(ctx, secondOrder.ID, courierID); err != nil {
		t.Fatalf("second COD assignment should stay within 500000 santim: %v", err)
	}
	thirdOrder, err := repo.Place(ctx, userID, "cod-at-cap-third", domain.CheckoutRequest{AddressID: addressID, PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: atCapItemID, Quantity: 1}}}, nil)
	if err != nil {
		t.Fatalf("third COD checkout rejected: %v", err)
	}
	if err := assignmentRepo.AssignCourier(ctx, thirdOrder.ID, courierID); !errors.Is(err, dispatchdomain.ErrCourierCODLimit) {
		t.Fatalf("expected courier cash limit rejection, got %v", err)
	}
}
