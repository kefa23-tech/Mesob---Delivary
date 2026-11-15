package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
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
	var codSuspended bool
	var completedOrders, unpaidCancellations int64
	if request.PaymentMethod == domain.PaymentCOD {
		var accountStatus string
		err = tx.QueryRow(ctx, `SELECT status,cod_suspended FROM users WHERE id=$1 AND role='customer' FOR UPDATE`, userID).Scan(&accountStatus, &codSuspended)
		if err == pgx.ErrNoRows || (err == nil && accountStatus != "active") {
			return domain.Order{}, domain.ErrInvalidOrder
		}
		if err != nil {
			return domain.Order{}, fmt.Errorf("lock COD customer: %w", err)
		}
		err = tx.QueryRow(ctx, `SELECT
count(*) FILTER (WHERE status='delivered'),
count(*) FILTER (WHERE status IN ('cancelled','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT') AND payment_method='COD' AND payment_status <> 'paid')
FROM orders WHERE user_id=$1`, userID).Scan(&completedOrders, &unpaidCancellations)
		if err != nil {
			return domain.Order{}, fmt.Errorf("load COD customer history: %w", err)
		}
		if err := domain.ValidateCODEligibility(0, completedOrders, unpaidCancellations, codSuspended); err != nil {
			return domain.Order{}, err
		}
	}

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

	paymentStatus := "pending"
	if request.PaymentMethod != domain.PaymentCOD {
		paymentStatus = "pending_payment"
	}
	order := domain.Order{UserID: userID, RestaurantID: restaurantID, AddressID: request.AddressID, Status: domain.OrderPending, PaymentMethod: request.PaymentMethod, PaymentStatus: paymentStatus, DeliveryFeeMinor: DeliveryFeeMinor, Currency: "ETB", Items: make([]domain.OrderItem, 0, len(lines))}
	if request.PaymentMethod == domain.PaymentCOD {
		order.Status = domain.OrderConfirmedCOD
	}
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
	if request.PaymentMethod == domain.PaymentCOD {
		if err := domain.ValidateCODEligibility(order.TotalMinor, completedOrders, unpaidCancellations, codSuspended); err != nil {
			return domain.Order{}, err
		}
	}

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
	deliveryPIN, err := newDeliveryPIN()
	if err != nil { return domain.Order{}, fmt.Errorf("generate delivery PIN: %w",err) }
	err = tx.QueryRow(ctx, `INSERT INTO orders(user_id, restaurant_id, delivery_address_id, status, payment_status, payment_method,
subtotal_minor, delivery_fee_minor, commission_minor, total_minor, currency, placed_at, idempotency_key)
VALUES($1,$2,$3,CASE WHEN $4='COD' THEN 'CONFIRMED_COD' ELSE 'pending' END,CASE WHEN $4='COD' THEN 'pending' ELSE 'pending_payment' END,$4,$5,$6,$7,$8,'ETB',now(),$9) RETURNING id::text, placed_at`, userID, restaurantID, request.AddressID, string(request.PaymentMethod), order.SubtotalMinor, order.DeliveryFeeMinor, order.CommissionMinor, order.TotalMinor, key).Scan(&order.ID, &placed)
	if err != nil {
		return domain.Order{}, fmt.Errorf("create order: %w", err)
	}
	pinHash:=sha256.Sum256([]byte(order.ID+":"+deliveryPIN))
	if _,err:=tx.Exec(ctx,`UPDATE orders SET delivery_pin_hash=$2 WHERE id=$1`,order.ID,pinHash[:]);err!=nil{return domain.Order{},fmt.Errorf("save delivery PIN hash: %w",err)}
	order.DeliveryPIN=deliveryPIN
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

func newDeliveryPIN()(string,error){n,err:=rand.Int(rand.Reader,big.NewInt(10_000));if err!=nil{return "",err};return fmt.Sprintf("%04d",n.Int64()),nil}

func (r *Repository) ListRestaurantOrders(ctx context.Context, partnerID string) ([]domain.KitchenOrder, error) {
	rows, err := r.pool.Query(ctx, `SELECT o.id::text,o.user_id::text,o.restaurant_id::text,o.delivery_address_id::text,o.status,o.payment_method,o.payment_status,o.subtotal_minor,o.delivery_fee_minor,o.commission_minor,o.total_minor,o.currency,o.placed_at,u.name,u.phone,o.prep_minutes,COALESCE(o.rejection_reason,'')
FROM orders o JOIN restaurants r ON r.id=o.restaurant_id JOIN users u ON u.id=o.user_id
WHERE r.owner_id=$1 AND u.role='restaurant_owner' AND o.status IN ('PAID','CONFIRMED_COD','ACCEPTED','PREPARING','READY_FOR_PICKUP','COURIER_ASSIGNED') ORDER BY o.placed_at`, partnerID)
	if err != nil {
		return nil, fmt.Errorf("list restaurant orders: %w", err)
	}
	defer rows.Close()
	orders := make([]domain.KitchenOrder, 0)
	for rows.Next() {
		var o domain.KitchenOrder
		var placed *time.Time
		var prep *int
		if err := rows.Scan(&o.ID, &o.UserID, &o.RestaurantID, &o.AddressID, &o.Status, &o.PaymentMethod, &o.PaymentStatus, &o.SubtotalMinor, &o.DeliveryFeeMinor, &o.CommissionMinor, &o.TotalMinor, &o.Currency, &placed, &o.CustomerName, &o.CustomerPhone, &prep, &o.RejectionReason); err != nil {
			return nil, err
		}
		if placed != nil {
			o.PlacedAt = placed.UTC().Format(time.RFC3339Nano)
		}
		o.PrepMinutes = prep
		itemRows, err := r.pool.Query(ctx, `SELECT id::text,menu_item_id::text,item_name,unit_price_minor,quantity,options,line_total_minor FROM order_items WHERE order_id=$1 ORDER BY created_at,id`, o.ID)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			var i domain.OrderItem
			var menuID *string
			var options []byte
			if err := itemRows.Scan(&i.ID, &menuID, &i.ItemName, &i.UnitPriceMinor, &i.Quantity, &options, &i.LineTotalMinor); err != nil {
				itemRows.Close()
				return nil, err
			}
			if menuID != nil {
				i.MenuItemID = *menuID
			}
			_ = json.Unmarshal(options, &i.Options)
			o.Items = append(o.Items, i)
		}
		itemRows.Close()
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *Repository) AcceptRestaurantOrder(ctx context.Context, partnerID, orderID string, prepMinutes int) (domain.KitchenOrder, error) {
	return r.transitionKitchenOrder(ctx, partnerID, orderID, []string{"PAID", "CONFIRMED_COD"}, domain.OrderAccepted, prepMinutes, "")
}
func (r *Repository) StartRestaurantOrder(ctx context.Context, partnerID, orderID string) (domain.KitchenOrder, error) {
	return r.transitionKitchenOrder(ctx, partnerID, orderID, []string{"ACCEPTED"}, domain.OrderKitchenPreparing, 0, "")
}
func (r *Repository) MarkRestaurantOrderReady(ctx context.Context, partnerID, orderID string) (domain.KitchenOrder, error) {
	tx,err:=r.pool.Begin(ctx);if err!=nil{return domain.KitchenOrder{},err};defer func(){_=tx.Rollback(context.Background())}()
	var o domain.KitchenOrder;var placed time.Time
	err=tx.QueryRow(ctx,`UPDATE orders o SET status=CASE WHEN o.courier_id IS NULL THEN 'READY_FOR_PICKUP' ELSE 'COURIER_ASSIGNED' END,ready_at=COALESCE(o.ready_at,now()),updated_at=now() FROM restaurants r JOIN users u ON u.id=r.owner_id WHERE o.id=$1 AND r.id=o.restaurant_id AND r.owner_id=$2 AND u.role='restaurant_owner' AND o.status IN ('PREPARING','COURIER_ASSIGNED') RETURNING o.id::text,o.user_id::text,o.restaurant_id::text,o.delivery_address_id::text,o.status,o.payment_method,o.payment_status,o.subtotal_minor,o.delivery_fee_minor,o.commission_minor,o.total_minor,o.currency,o.placed_at,COALESCE(o.prep_minutes,0)`,orderID,partnerID).Scan(&o.ID,&o.UserID,&o.RestaurantID,&o.AddressID,&o.Status,&o.PaymentMethod,&o.PaymentStatus,&o.SubtotalMinor,&o.DeliveryFeeMinor,&o.CommissionMinor,&o.TotalMinor,&o.Currency,&placed,&o.PrepMinutes)
	if err!=nil{return domain.KitchenOrder{},fmt.Errorf("mark order ready: %w",err)};o.PlacedAt=placed.UTC().Format(time.RFC3339Nano)
	if _,err=tx.Exec(ctx,`INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'courier.order_ready','{}'::jsonb) ON CONFLICT(order_id,event_type) DO NOTHING`,orderID);err!=nil{return domain.KitchenOrder{},err};if err=tx.Commit(ctx);err!=nil{return domain.KitchenOrder{},err};return o,nil
}
func (r *Repository) transitionKitchenOrder(ctx context.Context, partnerID, orderID string, from []string, next domain.OrderStatus, prep int, reason string) (domain.KitchenOrder, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.KitchenOrder{}, fmt.Errorf("begin kitchen transition: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var o domain.KitchenOrder
	var placed time.Time
	var prepValue int
	err = tx.QueryRow(ctx, `UPDATE orders o SET status=$3,prep_minutes=CASE WHEN $4>0 THEN $4 ELSE prep_minutes END,accepted_at=CASE WHEN $3='ACCEPTED' THEN now() ELSE accepted_at END,updated_at=now() FROM restaurants r JOIN users u ON u.id=r.owner_id WHERE o.id=$1 AND r.id=o.restaurant_id AND r.owner_id=$2 AND u.role='restaurant_owner' AND o.status=ANY($5::text[]) RETURNING o.id::text,o.user_id::text,o.restaurant_id::text,o.delivery_address_id::text,o.status,o.payment_method,o.payment_status,o.subtotal_minor,o.delivery_fee_minor,o.commission_minor,o.total_minor,o.currency,o.placed_at,COALESCE(o.prep_minutes,0)`, orderID, partnerID, string(next), prep, from).Scan(&o.ID, &o.UserID, &o.RestaurantID, &o.AddressID, &o.Status, &o.PaymentMethod, &o.PaymentStatus, &o.SubtotalMinor, &o.DeliveryFeeMinor, &o.CommissionMinor, &o.TotalMinor, &o.Currency, &placed, &prepValue)
	if err != nil {
		return domain.KitchenOrder{}, fmt.Errorf("transition kitchen order: %w", err)
	}
	o.PlacedAt = placed.UTC().Format(time.RFC3339Nano)
	if prepValue > 0 {
		o.PrepMinutes = &prepValue
	}
	if next == domain.OrderAccepted {
		_, err = tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'restaurant.dispatch_requested','{}'::jsonb) ON CONFLICT(order_id,event_type) DO NOTHING`, orderID)
	}
	if next == domain.OrderReadyForPickup {
		_, err = tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'courier.order_ready','{}'::jsonb) ON CONFLICT(order_id,event_type) DO NOTHING`, orderID)
	}
	if err != nil {
		return domain.KitchenOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.KitchenOrder{}, fmt.Errorf("commit kitchen transition: %w", err)
	}
	return o, nil
}

func (r *Repository) RejectRestaurantOrder(ctx context.Context, partnerID, orderID string, reason domain.RestaurantRejectionReason) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var method string
	err = tx.QueryRow(ctx, `UPDATE orders o SET status='REJECTED_BY_RESTAURANT',rejection_reason=$3,updated_at=now() FROM restaurants r JOIN users u ON u.id=r.owner_id WHERE o.id=$1 AND r.id=o.restaurant_id AND r.owner_id=$2 AND u.role='restaurant_owner' AND o.status IN ('PAID','CONFIRMED_COD','ACCEPTED','PREPARING') RETURNING o.payment_method`, orderID, partnerID, string(reason)).Scan(&method)
	if err != nil {
		return fmt.Errorf("reject restaurant order: %w", err)
	}
	if err := restoreOrderInventory(ctx, tx, orderID); err != nil {
		return err
	}
	if method != "COD" {
		_, err = tx.Exec(ctx, `UPDATE orders SET payment_status='refund_pending' WHERE id=$1 AND payment_status='paid'`, orderID)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'payment.refund_requested',jsonb_build_object('reason',$2)) ON CONFLICT(order_id,event_type) DO NOTHING`, orderID, string(reason))
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) TimeoutRestaurantOrder(ctx context.Context, orderID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var method string
	err = tx.QueryRow(ctx, `UPDATE orders SET status='CANCELLED_TIMEOUT',updated_at=now() WHERE id=$1 AND status IN ('PAID','CONFIRMED_COD') RETURNING payment_method`, orderID).Scan(&method)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err := restoreOrderInventory(ctx, tx, orderID); err != nil {
		return err
	}
	if method != "COD" {
		_, err = tx.Exec(ctx, `UPDATE orders SET payment_status='refund_pending' WHERE id=$1 AND payment_status='paid'`, orderID)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'payment.refund_requested',jsonb_build_object('reason','RESTAURANT_SLA_TIMEOUT')) ON CONFLICT(order_id,event_type) DO NOTHING`, orderID)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func restoreOrderInventory(ctx context.Context, tx pgx.Tx, orderID string) error {
	_, err := tx.Exec(ctx, `UPDATE menu_items mi SET stock_remaining=LEAST(mi.stock_limit,COALESCE(mi.stock_remaining,mi.stock_limit)+oi.quantity),updated_at=now() FROM (SELECT menu_item_id,SUM(quantity)::integer AS quantity FROM order_items WHERE order_id=$1 AND menu_item_id IS NOT NULL GROUP BY menu_item_id) oi WHERE mi.id=oi.menu_item_id AND mi.stock_limit IS NOT NULL`, orderID)
	return err
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
