//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"github.com/mesob-delivery/backend/internal/adapter/postgres"
	catalogpostgres "github.com/mesob-delivery/backend/internal/catalog/adapter/postgres"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchExcludesClosedAndOutOfRadiusRestaurants(t *testing.T) {
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
	migrationsPath, err := filepath.Abs("../../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.RunMigrations(ctx, databaseURL, migrationsPath); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var ownerID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (name, phone, role, status) VALUES ('Owner', '+251900000000', 'restaurant_owner', 'active') RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO restaurants (owner_id, name, lat, lng, delivery_radius_km, is_open, approval_status) VALUES ($1, 'Near Open', 9.0005, 38.7505, 5, true, 'approved'), ($1, 'Far Open', 9.1000, 38.7500, 5, true, 'approved'), ($1, 'Near Closed', 9.0002, 38.7502, 5, false, 'approved')`, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalogpostgres.NewRepository(pool).Search(ctx, domain.SearchFilter{Lat: 9, Lng: 38.75, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Restaurants) != 1 || result.Restaurants[0].Name != "Near Open" {
		t.Fatalf("unexpected restaurants: %+v", result.Restaurants)
	}
}
