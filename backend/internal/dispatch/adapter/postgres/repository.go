package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// AssignCourier assigns an order once. The database reservation trigger locks
// the courier and rejects COD assignments that would exceed their cash limit.
func (r *Repository) AssignCourier(ctx context.Context, orderID, courierID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin courier assignment: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	command, err := tx.Exec(ctx, `UPDATE orders SET courier_id=$2, updated_at=now()
WHERE id=$1 AND courier_id IS NULL AND status NOT IN ('delivered','cancelled')`, orderID, courierID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			if strings.Contains(pgErr.Message, "courier COD cash limit") {
				return domain.ErrCourierCODLimit
			}
			if strings.Contains(pgErr.Message, "courier is not active") {
				return domain.ErrCourierIneligible
			}
		}
		return fmt.Errorf("assign courier: %w", err)
	}
	if command.RowsAffected() != 1 {
		var assigned string
		err := tx.QueryRow(ctx, `SELECT COALESCE(courier_id::text,'') FROM orders WHERE id=$1`, orderID).Scan(&assigned)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrOrderNotAssignable
		}
		if err != nil {
			return fmt.Errorf("check courier assignment: %w", err)
		}
		if assigned == courierID {
			return tx.Commit(ctx)
		}
		return domain.ErrOrderNotAssignable
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit courier assignment: %w", err)
	}
	return nil
}
