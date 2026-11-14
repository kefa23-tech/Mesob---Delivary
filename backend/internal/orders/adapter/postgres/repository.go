package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/orders/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func (r *Repository) GetMenuItem(ctx context.Context, restaurantID, itemID string) (domain.MenuItem, error) {
	var item domain.MenuItem
	err := r.pool.QueryRow(ctx, `SELECT mi.id::text, mc.restaurant_id::text, mi.name, mi.price_minor FROM menu_items mi JOIN menu_categories mc ON mc.id = mi.category_id WHERE mi.id = $1 AND mc.restaurant_id = $2 AND mi.is_available = true`, itemID, restaurantID).Scan(&item.ID, &item.RestaurantID, &item.Name, &item.PriceMinor)
	if err != nil {
		return domain.MenuItem{}, fmt.Errorf("find menu item: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT og.id::text, og.name, og.min_select, og.max_select, o.id::text, o.name, o.price_delta_minor, o.is_available FROM menu_option_groups og LEFT JOIN menu_options o ON o.option_group_id = og.id WHERE og.menu_item_id = $1 ORDER BY og.sort_order, og.id, o.sort_order, o.id`, item.ID)
	if err != nil {
		return domain.MenuItem{}, fmt.Errorf("find item options: %w", err)
	}
	defer rows.Close()
	groups := map[string]int{}
	for rows.Next() {
		var groupID, groupName string
		var minSelect, maxSelect int
		var optionID, optionName *string
		var delta *int64
		var available *bool
		if err := rows.Scan(&groupID, &groupName, &minSelect, &maxSelect, &optionID, &optionName, &delta, &available); err != nil {
			return domain.MenuItem{}, fmt.Errorf("scan item options: %w", err)
		}
		index, exists := groups[groupID]
		if !exists {
			item.OptionGroups = append(item.OptionGroups, domain.OptionGroup{ID: groupID, Name: groupName, MinSelect: minSelect, MaxSelect: maxSelect})
			index = len(item.OptionGroups) - 1
			groups[groupID] = index
		}
		if optionID != nil {
			item.OptionGroups[index].Options = append(item.OptionGroups[index].Options, domain.Option{ID: *optionID, Name: *optionName, PriceDeltaMinor: *delta, IsAvailable: *available})
		}
	}
	if err := rows.Err(); err != nil {
		return domain.MenuItem{}, fmt.Errorf("iterate item options: %w", err)
	}
	return item, nil
}
