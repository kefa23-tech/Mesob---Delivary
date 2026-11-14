package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Search(ctx context.Context, filter domain.SearchFilter) (domain.SearchResult, error) {
	const query = `WITH nearby AS (
SELECT r.id::text, r.name, r.lat::double precision, r.lng::double precision,
       r.delivery_radius_km::double precision, r.is_open, r.eco_packaging_certified,
       r.cuisines, ST_Distance(r.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography) AS distance_m
FROM restaurants r
WHERE r.approval_status = 'approved'
  AND r.is_open = true
  AND ST_DWithin(r.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, r.delivery_radius_km * 1000)
  AND ($3 = '' OR EXISTS (SELECT 1 FROM unnest(r.cuisines) c WHERE lower(c) = lower($3)))
  AND ($4 = '' OR lower(r.name) LIKE lower('%' || $4 || '%') OR EXISTS (
      SELECT 1 FROM menu_categories mc JOIN menu_items mi ON mi.category_id = mc.id
      WHERE mc.restaurant_id = r.id AND mi.is_available = true AND lower(mi.name) LIKE lower('%' || $4 || '%')
  ))
  AND ($5::boolean IS NULL OR r.eco_packaging_certified = $5)
)
SELECT id, name, lat, lng, delivery_radius_km, is_open, eco_packaging_certified, cuisines, distance_m
FROM nearby
WHERE ($6::double precision IS NULL OR (distance_m, id::uuid) > ($6::double precision, $7::uuid))
ORDER BY distance_m ASC, id ASC
LIMIT $8`

	var afterDistance any
	var afterID any
	if filter.Cursor.RestaurantID != "" {
		afterDistance = filter.Cursor.DistanceMeters
		afterID = string(filter.Cursor.RestaurantID)
	}
	rows, err := r.pool.Query(ctx, query, filter.Lat, filter.Lng, filter.Cuisine, filter.Query, filter.EcoPackaging, afterDistance, afterID, filter.Limit+1)
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("search restaurants: %w", err)
	}
	defer rows.Close()
	result := domain.SearchResult{Restaurants: make([]domain.Restaurant, 0, filter.Limit+1)}
	for rows.Next() {
		var restaurant domain.Restaurant
		var id string
		var cuisines []string
		if err := rows.Scan(&id, &restaurant.Name, &restaurant.Lat, &restaurant.Lng, &restaurant.DeliveryRadiusKm, &restaurant.IsOpen, &restaurant.EcoPackagingCertified, &cuisines, &restaurant.DistanceMeters); err != nil {
			return domain.SearchResult{}, fmt.Errorf("scan restaurant: %w", err)
		}
		restaurant.ID = domain.RestaurantID(id)
		restaurant.Cuisines = cuisines
		result.Restaurants = append(result.Restaurants, restaurant)
	}
	if err := rows.Err(); err != nil {
		return domain.SearchResult{}, fmt.Errorf("iterate restaurants: %w", err)
	}
	return result, nil
}
