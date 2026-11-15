ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'pending','confirmed','preparing','ready','dispatched','delivered','cancelled',
  'PAID','CONFIRMED_COD','ACCEPTED','PREPARING','READY_FOR_PICKUP',
  'REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT'
));
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check CHECK (payment_status IN (
  'pending','pending_payment','authorized','paid','payment_failed','failed','refunded','refund_pending'
));
ALTER TABLE orders ADD COLUMN prep_minutes INTEGER CHECK (prep_minutes BETWEEN 1 AND 240);
ALTER TABLE orders ADD COLUMN accepted_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN rejection_reason TEXT CHECK (rejection_reason IS NULL OR rejection_reason IN ('OUT_OF_STOCK','TOO_BUSY','RESTAURANT_CLOSED','OTHER'));
CREATE INDEX orders_restaurant_kitchen_idx ON orders(restaurant_id,status,placed_at) WHERE status IN ('PAID','CONFIRMED_COD','ACCEPTED','PREPARING','READY_FOR_PICKUP');

CREATE FUNCTION enforce_order_lifecycle_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = NEW.status THEN RETURN NEW; END IF;
  IF NOT (
    (OLD.status IN ('pending','pending_payment') AND NEW.status IN ('confirmed','PAID','CONFIRMED_COD','cancelled')) OR
    (OLD.status IN ('PAID','CONFIRMED_COD') AND NEW.status IN ('ACCEPTED','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT')) OR
    (OLD.status = 'ACCEPTED' AND NEW.status IN ('PREPARING','REJECTED_BY_RESTAURANT')) OR
    (OLD.status = 'PREPARING' AND NEW.status IN ('READY_FOR_PICKUP','REJECTED_BY_RESTAURANT')) OR
    (OLD.status = 'READY_FOR_PICKUP' AND NEW.status = 'dispatched') OR
    (OLD.status = 'confirmed' AND NEW.status IN ('preparing','cancelled')) OR
    (OLD.status = 'preparing' AND NEW.status IN ('ready','cancelled')) OR
    (OLD.status = 'ready' AND NEW.status IN ('dispatched','cancelled')) OR
    (OLD.status = 'dispatched' AND NEW.status = 'delivered')
  ) THEN
    RAISE EXCEPTION 'invalid order status transition: % -> %', OLD.status, NEW.status USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER orders_lifecycle_transition BEFORE UPDATE OF status ON orders
FOR EACH ROW EXECUTE FUNCTION enforce_order_lifecycle_transition();

DROP TRIGGER IF EXISTS orders_release_cod_reservation ON orders;
CREATE TRIGGER orders_release_cod_reservation
AFTER UPDATE OF status ON orders
FOR EACH ROW WHEN (NEW.status IN ('cancelled','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT') AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION release_cod_reservation_on_cancel();
