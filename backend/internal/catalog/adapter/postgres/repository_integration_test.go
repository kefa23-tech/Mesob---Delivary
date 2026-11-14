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
	"strings"
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
	if err := runMigrationsWhenReady(ctx, databaseURL, migrationsPath); err != nil {
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

func TestMenuReturnsNestedOptionConstraintsAndPriceDeltas(t *testing.T) {
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
	if err := runMigrationsWhenReady(ctx, databaseURL, migrationsPath); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var ownerID, restaurantID, categoryID, itemID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (name, phone, role, status) VALUES ('Owner', '+251900000001', 'restaurant_owner', 'active') RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants (owner_id, name, lat, lng, is_open, approval_status) VALUES ($1, 'Menu House', 9, 38.75, true, 'approved') RETURNING id::text`, ownerID).Scan(&restaurantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_categories (restaurant_id, name) VALUES ($1, 'Mains') RETURNING id::text`, restaurantID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items (category_id, name, description, image_url, price_minor, is_available) VALUES ($1, 'Shiro', 'Spiced chickpeas', 'https://cdn.example/shiro.webp', 12500, true) RETURNING id::text`, categoryID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var groupID string
	if err := pool.QueryRow(ctx, `INSERT INTO menu_option_groups (menu_item_id, name, min_select, max_select) VALUES ($1, 'Spice level', 1, 2) RETURNING id::text`, itemID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO menu_options (option_group_id, name, price_delta_minor, is_available) VALUES ($1, 'Mild', 0, true), ($1, 'Extra hot', 500, true)`, groupID); err != nil {
		t.Fatal(err)
	}
	menu, err := catalogpostgres.NewRepository(pool).GetMenu(ctx, domain.RestaurantID(restaurantID))
	if err != nil {
		t.Fatal(err)
	}
	if len(menu.Categories) != 1 || len(menu.Categories[0].Items) != 1 {
		t.Fatalf("unexpected nested menu: %+v", menu)
	}
	item := menu.Categories[0].Items[0]
	if item.ImageURL == "" || item.PriceMinor != 12500 || len(item.OptionGroups) != 1 {
		t.Fatalf("unexpected item: %+v", item)
	}
	group := item.OptionGroups[0]
	var extraHotFound bool
	for _, option := range group.Options {
		if option.Name == "Extra hot" && option.PriceDeltaMinor == 500 {
			extraHotFound = true
		}
	}
	if group.MinSelect != 1 || group.MaxSelect != 2 || len(group.Options) != 2 || !extraHotFound {
		t.Fatalf("unexpected option group: %+v", group)
	}
}

func runMigrationsWhenReady(ctx context.Context, databaseURL, migrationsPath string) error {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		lastErr = postgres.RunMigrations(ctx, databaseURL, migrationsPath)
		if lastErr == nil {
			return nil
		}
		message := lastErr.Error()
		if !strings.Contains(message, "database system is starting up") &&
			!strings.Contains(message, "connection refused") &&
			!strings.Contains(message, "connection reset") {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return lastErr
}
