DROP INDEX IF EXISTS orders_restaurant_kitchen_idx;
DROP TRIGGER IF EXISTS orders_lifecycle_transition ON orders;
DROP FUNCTION IF EXISTS enforce_order_lifecycle_transition();
DROP TRIGGER IF EXISTS orders_release_cod_reservation ON orders;
CREATE TRIGGER orders_release_cod_reservation
AFTER UPDATE OF status ON orders
FOR EACH ROW WHEN (NEW.status = 'cancelled' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION release_cod_reservation_on_cancel();
ALTER TABLE orders DROP COLUMN IF EXISTS rejection_reason;
ALTER TABLE orders DROP COLUMN IF EXISTS accepted_at;
ALTER TABLE orders DROP COLUMN IF EXISTS prep_minutes;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check CHECK (payment_status IN ('pending','pending_payment','authorized','paid','payment_failed','failed','refunded'));
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN ('pending','confirmed','preparing','ready','dispatched','delivered','cancelled'));
