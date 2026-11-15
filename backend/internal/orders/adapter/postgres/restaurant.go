package postgres

import "context"

func (r *Repository) RestaurantIDForOwner(ctx context.Context, ownerID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT id::text FROM restaurants WHERE owner_id=$1 ORDER BY created_at LIMIT 1`, ownerID).Scan(&id)
	return id, err
}
