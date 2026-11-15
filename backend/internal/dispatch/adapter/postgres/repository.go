package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
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

func(r *Repository)UpdateCourierStatus(ctx context.Context,id domain.CourierID,online bool,vehicle domain.VehicleType)error{
	if !vehicle.Valid(){return domain.ErrInvalidCourier}
	cmd,err:=r.pool.Exec(ctx,`INSERT INTO courier_presence(courier_id,online,vehicle_type,updated_at) SELECT id,$2,$3,now() FROM users WHERE id=$1 AND role='courier' AND status='active' ON CONFLICT(courier_id) DO UPDATE SET online=EXCLUDED.online,vehicle_type=EXCLUDED.vehicle_type,updated_at=now()`,string(id),online,string(vehicle));if err!=nil{return err};if cmd.RowsAffected()!=1{return domain.ErrCourierIneligible};return nil
}

func(r *Repository)EnsureCourierOnline(ctx context.Context,id domain.CourierID)error{var online bool;err:=r.pool.QueryRow(ctx,`SELECT cp.online FROM courier_presence cp JOIN users u ON u.id=cp.courier_id WHERE cp.courier_id=$1 AND u.role='courier' AND u.status='active'`,string(id)).Scan(&online);if err!=nil||!online{return domain.ErrCourierIneligible};return nil}

func(r *Repository)DispatchOrder(ctx context.Context,id domain.OrderID)(domain.Order,error){var o domain.Order;var courier *string;err:=r.pool.QueryRow(ctx,`SELECT o.id::text,r.lat,r.lng,o.payment_method,o.total_minor,o.status,COALESCE(o.restaurant_status,o.status),o.courier_id::text FROM orders o JOIN restaurants r ON r.id=o.restaurant_id WHERE o.id=$1`,string(id)).Scan(&o.ID,&o.RestaurantLat,&o.RestaurantLng,&o.PaymentMethod,&o.TotalMinor,&o.Status,&o.KitchenStatus,&courier);if err!=nil{return o,err};if courier!=nil{o.CourierID=domain.CourierID(*courier)};return o,nil}

func(r *Repository)EligibleCouriers(ctx context.Context,order domain.OrderID,ids []domain.CourierID)([]domain.CourierID,error){if len(ids)==0{return nil,nil};raw:=make([]string,0,len(ids));for _,id:=range ids{raw=append(raw,string(id))};o,err:=r.DispatchOrder(ctx,order);if err!=nil{return nil,err};rows,err:=r.pool.Query(ctx,`SELECT u.id::text FROM users u JOIN courier_presence cp ON cp.courier_id=u.id AND cp.online=true LEFT JOIN courier_cash_balances b ON b.courier_id=u.id WHERE u.id=ANY($1::uuid[]) AND u.role='courier' AND u.status='active' AND NOT EXISTS(SELECT 1 FROM orders active WHERE active.courier_id=u.id AND active.status IN ('COURIER_ASSIGNED','PICKED_UP','dispatched')) AND ($2<>'COD' OR COALESCE(b.balance_minor,0)+COALESCE(b.reserved_minor,0)+$3<=u.courier_cod_limit_minor)`,raw,o.PaymentMethod,o.TotalMinor);if err!=nil{return nil,err};defer rows.Close();eligible:=make([]domain.CourierID,0);for rows.Next(){var id string;if err:=rows.Scan(&id);err!=nil{return nil,err};eligible=append(eligible,domain.CourierID(id))};return eligible,rows.Err()}

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

func(r *Repository)ClaimCourier(ctx context.Context,orderID domain.OrderID,courierID domain.CourierID)error{
	tx,err:=r.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer func(){_=tx.Rollback(context.Background())}()
	cmd,err:=tx.Exec(ctx,`UPDATE orders SET courier_id=$2,status='COURIER_ASSIGNED',courier_assigned_at=now(),updated_at=now() WHERE id=$1 AND courier_id IS NULL AND restaurant_status IN ('ACCEPTED','PREPARING','READY_FOR_PICKUP') AND status IN ('ACCEPTED','PREPARING','READY_FOR_PICKUP') AND EXISTS(SELECT 1 FROM courier_presence cp JOIN users u ON u.id=cp.courier_id WHERE cp.courier_id=$2 AND cp.online AND u.role='courier' AND u.status='active')`,string(orderID),string(courierID));if err!=nil{return assignmentError(err)}
	if cmd.RowsAffected()!=1 { var assigned string;err=tx.QueryRow(ctx,`SELECT COALESCE(courier_id::text,'') FROM orders WHERE id=$1`,string(orderID)).Scan(&assigned);if err!=nil{return domain.ErrOrderNotAssignable};if assigned==string(courierID){return tx.Commit(ctx)};return domain.ErrOrderClaimed }
	if _,err=tx.Exec(ctx,`INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'dispatch.courier_assigned',jsonb_build_object('courier_id',$2)) ON CONFLICT(order_id,event_type) DO NOTHING`,string(orderID),string(courierID));err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err};return nil
}
func assignmentError(err error)error{var pgErr *pgconn.PgError;if errors.As(err,&pgErr)&&pgErr.Code=="23514"{if strings.Contains(pgErr.Message,"courier COD cash limit"){return domain.ErrCourierCODLimit};if strings.Contains(pgErr.Message,"courier is not active"){return domain.ErrCourierIneligible}};return fmt.Errorf("claim courier assignment: %w",err)}

func(r *Repository)Pickup(ctx context.Context,orderID domain.OrderID,courierID domain.CourierID)error{cmd,err:=r.pool.Exec(ctx,`UPDATE orders SET status='PICKED_UP',picked_up_at=now(),updated_at=now() WHERE id=$1 AND courier_id=$2 AND status='COURIER_ASSIGNED' AND restaurant_status='READY_FOR_PICKUP' AND ready_at IS NOT NULL`,string(orderID),string(courierID));if err!=nil{return err};if cmd.RowsAffected()!=1{return domain.ErrOrderNotAssignable};return nil}
func(r *Repository)Complete(ctx context.Context,orderID domain.OrderID,courierID domain.CourierID,pin string)error{
	tx,err:=r.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer func(){_=tx.Rollback(context.Background())}();var hash []byte;var status,courier string
	err=tx.QueryRow(ctx,`SELECT COALESCE(courier_id::text,''),status,delivery_pin_hash FROM orders WHERE id=$1 FOR UPDATE`,string(orderID)).Scan(&courier,&status,&hash);if err!=nil{return err};if courier!=string(courierID)||status!="PICKED_UP"{return domain.ErrOrderNotAssignable};candidate:=sha256.Sum256([]byte(string(orderID)+":"+pin));if len(hash)!=len(candidate)||subtle.ConstantTimeCompare(hash,candidate[:])!=1{return domain.ErrInvalidPIN}
	cmd,err:=tx.Exec(ctx,`UPDATE orders SET status='DELIVERED',delivered_at=now(),updated_at=now() WHERE id=$1 AND courier_id=$2 AND status='PICKED_UP'`,string(orderID),string(courierID));if err!=nil{return err};if cmd.RowsAffected()!=1{return domain.ErrOrderNotAssignable};return tx.Commit(ctx)
}
func(r *Repository)MarkManualIntervention(ctx context.Context,orderID domain.OrderID)error{_,err:=r.pool.Exec(ctx,`INSERT INTO order_events(order_id,event_type,payload) VALUES($1,'dispatch.manual_intervention','{}'::jsonb) ON CONFLICT(order_id,event_type) DO NOTHING`,string(orderID));return err}
