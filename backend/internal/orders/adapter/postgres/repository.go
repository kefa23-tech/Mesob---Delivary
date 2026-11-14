package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/orders/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const DeliveryFeeMinor int64 = 15_000

type lockedItem struct {
	name, restaurantID         string
	price                      int64
	available                  bool
	stockLimit, stockRemaining *int32
	menu                       domain.MenuItem
}

func (r *Repository) Place(ctx context.Context, userID, key string, request domain.CheckoutRequest, groupItems []domain.CartItem) (domain.Order, error) {
	lines := make([]domain.CheckoutItem, 0, len(request.Items)+len(groupItems))
	if request.GroupCartToken != "" {
		for _, item := range groupItems {
			lines = append(lines, domain.CheckoutItem{MenuItemID: item.MenuItemID, Quantity: item.Quantity, Options: item.Options})
		}
	} else {
		lines = append(lines, request.Items...)
	}
	if len(lines) == 0 {
		return domain.Order{}, domain.ErrInvalidOrder
	}

	qtyByItem := make(map[string]int)
	ids := make([]string, 0)
	for _, line := range lines {
		if line.MenuItemID == "" || line.Quantity <= 0 {
			return domain.Order{}, domain.ErrInvalidOrder
		}
		if _, exists := qtyByItem[line.MenuItemID]; !exists {
			ids = append(ids, line.MenuItemID)
		}
		qtyByItem[line.MenuItemID] += line.Quantity
	}
	sort.Strings(ids)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Order{}, fmt.Errorf("begin checkout: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	items := make(map[string]*lockedItem, len(ids))
	rows, err := tx.Query(ctx, `SELECT mi.id::text, mc.restaurant_id::text, mi.name, mi.price_minor, mi.is_available, mi.stock_limit, mi.stock_remaining
FROM menu_items mi JOIN menu_categories mc ON mc.id=mi.category_id
WHERE mi.id = ANY($1::uuid[]) ORDER BY mi.id FOR UPDATE OF mi`, ids)
	if err != nil {
		return domain.Order{}, fmt.Errorf("lock menu items: %w", err)
	}
	for rows.Next() {
		var id string
		var item lockedItem
		if err := rows.Scan(&id, &item.restaurantID, &item.name, &item.price, &item.available, &item.stockLimit, &item.stockRemaining); err != nil {
			rows.Close()
			return domain.Order{}, fmt.Errorf("scan locked item: %w", err)
		}
		item.menu = domain.MenuItem{ID: id, RestaurantID: item.restaurantID, Name: item.name, PriceMinor: item.price}
		items[id] = &item
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.Order{}, fmt.Errorf("read locked items: %w", err)
	}
	rows.Close()
	if len(items) != len(ids) {
		return domain.Order{}, domain.ErrItemUnavailable
	}
	restaurantID := ""
	for _, id := range ids {
		item := items[id]
		if restaurantID == "" {
			restaurantID = item.restaurantID
		}
		if restaurantID != item.restaurantID {
			return domain.Order{}, domain.ErrRestaurantMismatch
		}
		if !item.available {
			return domain.Order{}, domain.ErrItemUnavailable
		}
		if item.stockLimit != nil {
			remaining := *item.stockLimit
			if item.stockRemaining != nil {
				remaining = *item.stockRemaining
			}
			if int64(remaining) < int64(qtyByItem[id]) {
				return domain.Order{}, domain.ErrInsufficientStock
			}
		}
	}

	var open, inRange bool
	var commissionBasisPoints int64
	err = tx.QueryRow(ctx, `SELECT r.is_open, (r.commission_pct * 100)::bigint,
ST_DWithin(r.location, ST_SetSRID(ST_MakePoint(a.lng, a.lat), 4326)::geography, r.delivery_radius_km * 1000)
FROM restaurants r JOIN addresses a ON a.id=$2 AND a.user_id=$3
WHERE r.id=$1 AND r.approval_status='approved' FOR SHARE OF r`, restaurantID, request.AddressID, userID).Scan(&open, &commissionBasisPoints, &inRange)
	if err == pgx.ErrNoRows {
		return domain.Order{}, domain.ErrAddressNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("validate delivery: %w", err)
	}
	if !open {
		return domain.Order{}, domain.ErrRestaurantClosed
	}
	if !inRange {
		return domain.Order{}, domain.ErrAddressOutOfRange
	}

	// Load option groups and choices after inventory locks so the price snapshot is
	// derived from current database values, never from client supplied prices.
	for _, id := range ids {
		optionRows, err := tx.Query(ctx, `SELECT og.id::text, og.name, og.min_select, og.max_select,
o.id::text, o.name, o.price_delta_minor, o.is_available
FROM menu_option_groups og LEFT JOIN menu_options o ON o.option_group_id=og.id
WHERE og.menu_item_id=$1 ORDER BY og.sort_order, og.id, o.sort_order, o.id`, id)
		if err != nil {
			return domain.Order{}, fmt.Errorf("load checkout options: %w", err)
		}
		groupIndices := map[string]int{}
		for optionRows.Next() {
			var groupID, groupName string
			var minSelect, maxSelect int
			var optionID, optionName *string
			var delta *int64
			var available *bool
			if err := optionRows.Scan(&groupID, &groupName, &minSelect, &maxSelect, &optionID, &optionName, &delta, &available); err != nil {
				optionRows.Close()
				return domain.Order{}, fmt.Errorf("scan checkout option: %w", err)
			}
			groupIndex, exists := groupIndices[groupID]
			if !exists {
				item := items[id]
				item.menu.OptionGroups = append(item.menu.OptionGroups, domain.OptionGroup{ID: groupID, Name: groupName, MinSelect: minSelect, MaxSelect: maxSelect})
				groupIndex = len(item.menu.OptionGroups) - 1
				groupIndices[groupID] = groupIndex
			}
			if optionID != nil {
				items[id].menu.OptionGroups[groupIndex].Options = append(items[id].menu.OptionGroups[groupIndex].Options, domain.Option{ID: *optionID, Name: *optionName, PriceDeltaMinor: *delta, IsAvailable: *available})
			}
		}
		if err := optionRows.Err(); err != nil {
			optionRows.Close()
			return domain.Order{}, fmt.Errorf("read checkout options: %w", err)
		}
		optionRows.Close()
	}

	order := domain.Order{UserID: userID, RestaurantID: restaurantID, AddressID: request.AddressID, Status: domain.OrderPending, PaymentMethod: request.PaymentMethod, PaymentStatus: "pending", DeliveryFeeMinor: DeliveryFeeMinor, Currency: "ETB", Items: make([]domain.OrderItem, 0, len(lines))}
	for _, line := range lines {
		item := items[line.MenuItemID]
		if !domain.ValidateSelections(item.menu, line.Options) {
			return domain.Order{}, domain.ErrInvalidSelection
		}
		unit := item.price
		for _, option := range line.Options {
			unit += option.PriceDeltaMinor
		}
		lineTotal := int64(line.Quantity) * unit
		order.SubtotalMinor += lineTotal
		order.Items = append(order.Items, domain.OrderItem{MenuItemID: line.MenuItemID, ItemName: item.name, UnitPriceMinor: unit, Quantity: line.Quantity, Options: line.Options, LineTotalMinor: lineTotal})
	}
	order.CommissionMinor = order.SubtotalMinor/10_000*commissionBasisPoints + (order.SubtotalMinor%10_000*commissionBasisPoints+5_000)/10_000
	order.TotalMinor = order.SubtotalMinor + order.DeliveryFeeMinor

	// Inventory is decremented only after every line has passed validation; locks
	// make the check and update indivisible relative to competing checkouts.
	for _, id := range ids {
		if items[id].stockLimit == nil {
			continue
		}
		result, err := tx.Exec(ctx, `UPDATE menu_items SET stock_remaining=COALESCE(stock_remaining, stock_limit)-$2, updated_at=now()
WHERE id=$1 AND stock_limit IS NOT NULL AND COALESCE(stock_remaining, stock_limit)>=$2`, id, qtyByItem[id])
		if err != nil {
			return domain.Order{}, fmt.Errorf("decrement inventory: %w", err)
		}
		if result.RowsAffected() != 1 {
			return domain.Order{}, domain.ErrInsufficientStock
		}
	}
	var placed time.Time
	err = tx.QueryRow(ctx, `INSERT INTO orders(user_id, restaurant_id, delivery_address_id, status, payment_status, payment_method,
subtotal_minor, delivery_fee_minor, commission_minor, total_minor, currency, placed_at, idempotency_key)
VALUES($1,$2,$3,'pending','pending',$4,$5,$6,$7,$8,'ETB',now(),$9) RETURNING id::text, placed_at`, userID, restaurantID, request.AddressID, string(request.PaymentMethod), order.SubtotalMinor, order.DeliveryFeeMinor, order.CommissionMinor, order.TotalMinor, key).Scan(&order.ID, &placed)
	if err != nil {
		return domain.Order{}, fmt.Errorf("create order: %w", err)
	}
	order.PlacedAt = placed.UTC().Format(time.RFC3339Nano)
	for index := range order.Items {
		item := &order.Items[index]
		optionsJSON, _ := json.Marshal(item.Options)
		if err := tx.QueryRow(ctx, `INSERT INTO order_items(order_id, menu_item_id, item_name, unit_price_minor, quantity, options, line_total_minor)
VALUES($1,$2,$3,$4,$5,$6::jsonb,$7) RETURNING id::text`, order.ID, item.MenuItemID, item.ItemName, item.UnitPriceMinor, item.Quantity, string(optionsJSON), item.LineTotalMinor).Scan(&item.ID); err != nil {
			return domain.Order{}, fmt.Errorf("create order item: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, fmt.Errorf("commit checkout: %w", err)
	}
	return order, nil
}
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
