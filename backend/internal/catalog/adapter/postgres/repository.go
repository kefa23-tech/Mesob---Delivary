package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) GetMenu(ctx context.Context, restaurantID domain.RestaurantID) (domain.Menu, error) {
	var menu domain.Menu
	var id string
	var open bool
	err := r.pool.QueryRow(ctx, `SELECT id::text, name, is_open FROM restaurants WHERE id = $1 AND approval_status = 'approved'`, string(restaurantID)).Scan(&id, &menu.RestaurantName, &open)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Menu{}, domain.ErrRestaurantNotFound
	}
	if err != nil {
		return domain.Menu{}, fmt.Errorf("load restaurant: %w", err)
	}
	if !open {
		return domain.Menu{}, domain.ErrRestaurantClosed
	}
	menu.RestaurantID = domain.RestaurantID(id)
	menu.Categories = make([]domain.MenuCategory, 0)

	rows, err := r.pool.Query(ctx, `SELECT mc.id::text, mc.name, COALESCE(mi.id::text, ''), COALESCE(mi.name, ''), COALESCE(mi.description, ''), COALESCE(mi.image_url, ''), COALESCE(mi.price_minor, 0), COALESCE(mi.is_available, false) FROM menu_categories mc LEFT JOIN menu_items mi ON mi.category_id = mc.id WHERE mc.restaurant_id = $1 ORDER BY mc.sort_order, mc.id, mi.sort_order, mi.id`, string(restaurantID))
	if err != nil {
		return domain.Menu{}, fmt.Errorf("load menu categories: %w", err)
	}
	categoryIndex := make(map[string]int)
	itemIndex := make(map[string]struct{ category, item int })
	for rows.Next() {
		var categoryID, categoryName, itemID, itemName, description, imageURL string
		var price int64
		var available bool
		if err := rows.Scan(&categoryID, &categoryName, &itemID, &itemName, &description, &imageURL, &price, &available); err != nil {
			rows.Close()
			return domain.Menu{}, fmt.Errorf("scan menu item: %w", err)
		}
		categoryPosition, exists := categoryIndex[categoryID]
		if !exists {
			menu.Categories = append(menu.Categories, domain.MenuCategory{ID: domain.CategoryID(categoryID), Name: categoryName, Items: make([]domain.MenuItem, 0)})
			categoryPosition = len(menu.Categories) - 1
			categoryIndex[categoryID] = categoryPosition
		}
		if itemID != "" {
			menu.Categories[categoryPosition].Items = append(menu.Categories[categoryPosition].Items, domain.MenuItem{ID: domain.MenuItemID(itemID), Name: itemName, Description: description, ImageURL: imageURL, PriceMinor: price, IsAvailable: available, OptionGroups: make([]domain.OptionGroup, 0)})
			itemIndex[itemID] = struct{ category, item int }{category: categoryPosition, item: len(menu.Categories[categoryPosition].Items) - 1}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.Menu{}, fmt.Errorf("iterate menu items: %w", err)
	}
	rows.Close()

	optionRows, err := r.pool.Query(ctx, `SELECT og.id::text, og.menu_item_id::text, og.name, og.min_select, og.max_select, COALESCE(o.id::text, ''), COALESCE(o.name, ''), COALESCE(o.price_delta_minor, 0), COALESCE(o.is_available, false) FROM menu_option_groups og LEFT JOIN menu_options o ON o.option_group_id = og.id WHERE og.menu_item_id IN (SELECT mi.id FROM menu_items mi JOIN menu_categories mc ON mc.id = mi.category_id WHERE mc.restaurant_id = $1) ORDER BY og.sort_order, og.id, o.sort_order, o.name, o.id`, string(restaurantID))
	if err != nil {
		return domain.Menu{}, fmt.Errorf("load menu options: %w", err)
	}
	groupIndex := make(map[string]struct{ category, item, group int })
	for optionRows.Next() {
		var groupID, itemID, groupName, optionID, optionName string
		var minSelect, maxSelect int
		var priceDelta int64
		var available bool
		if err := optionRows.Scan(&groupID, &itemID, &groupName, &minSelect, &maxSelect, &optionID, &optionName, &priceDelta, &available); err != nil {
			optionRows.Close()
			return domain.Menu{}, fmt.Errorf("scan menu options: %w", err)
		}
		location, exists := itemIndex[itemID]
		if !exists {
			continue
		}
		item := &menu.Categories[location.category].Items[location.item]
		group, exists := groupIndex[groupID]
		if !exists {
			item.OptionGroups = append(item.OptionGroups, domain.OptionGroup{ID: groupID, Name: groupName, MinSelect: minSelect, MaxSelect: maxSelect, Options: make([]domain.Option, 0)})
			group = struct{ category, item, group int }{category: location.category, item: location.item, group: len(item.OptionGroups) - 1}
			groupIndex[groupID] = group
		}
		item = &menu.Categories[group.category].Items[group.item]
		if optionID != "" {
			item.OptionGroups[group.group].Options = append(item.OptionGroups[group.group].Options, domain.Option{ID: optionID, Name: optionName, PriceDeltaMinor: priceDelta, IsAvailable: available})
		}
	}
	if err := optionRows.Err(); err != nil {
		optionRows.Close()
		return domain.Menu{}, fmt.Errorf("iterate menu options: %w", err)
	}
	optionRows.Close()
	return menu, nil
}

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
