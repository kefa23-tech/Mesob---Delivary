package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/mesob-delivery/backend/internal/orders/domain"
)

func (r *Repository) GetTrackingSnapshot(ctx context.Context, userID, orderID string) (domain.TrackingSnapshot, error) {
	var s domain.TrackingSnapshot
	err := r.pool.QueryRow(ctx, `SELECT o.id::text,o.status,o.payment_status,COALESCE(o.restaurant_status,''),o.restaurant_id::text,r.name,
 r.lat,r.lng,a.lat,a.lng,a.line1,COALESCE(o.picked_up_at + interval '10 minutes',o.courier_assigned_at + interval '15 minutes'),
COALESCE(o.courier_id::text,''),COALESCE(u.name,''),COALESCE(u.phone,''),COALESCE(cp.vehicle_type,'')
FROM orders o JOIN restaurants r ON r.id=o.restaurant_id JOIN addresses a ON a.id=o.delivery_address_id
LEFT JOIN users u ON u.id=o.courier_id LEFT JOIN courier_presence cp ON cp.courier_id=o.courier_id
WHERE o.id=$1 AND o.user_id=$2`, orderID, userID).Scan(&s.ID, &s.Status, &s.PaymentStatus, &s.RestaurantStatus, &s.RestaurantID, &s.RestaurantName, &s.RestaurantLat, &s.RestaurantLng, &s.DeliveryLat, &s.DeliveryLng, &s.DeliveryAddress, &s.ETAAt, &s.CourierID, &s.CourierName, &s.CourierPhone, &s.CourierVehicle)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.ErrOrderNotFound
	}
	if err != nil {
		return s, fmt.Errorf("load order tracking snapshot: %w", err)
	}
	return s, nil
}
