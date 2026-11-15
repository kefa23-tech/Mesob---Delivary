DROP TABLE IF EXISTS courier_presence;
DROP TRIGGER IF EXISTS orders_restaurant_fulfillment_transition ON orders;
DROP FUNCTION IF EXISTS enforce_restaurant_fulfillment_transition();
DROP TRIGGER IF EXISTS orders_post_delivery_ledger ON orders;
CREATE TRIGGER orders_post_delivery_ledger AFTER UPDATE OF status ON orders
FOR EACH ROW WHEN (NEW.status='delivered' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION post_order_delivery_ledger();
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'pending','confirmed','preparing','ready','dispatched','delivered','cancelled',
  'PAID','CONFIRMED_COD','ACCEPTED','PREPARING','READY_FOR_PICKUP','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT'
));
CREATE OR REPLACE FUNCTION enforce_order_lifecycle_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = NEW.status THEN RETURN NEW; END IF;
  IF NOT ((OLD.status IN ('pending','pending_payment') AND NEW.status IN ('confirmed','PAID','CONFIRMED_COD','cancelled')) OR
    (OLD.status IN ('PAID','CONFIRMED_COD') AND NEW.status IN ('ACCEPTED','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT')) OR
    (OLD.status='ACCEPTED' AND NEW.status IN ('PREPARING','REJECTED_BY_RESTAURANT')) OR
    (OLD.status='PREPARING' AND NEW.status IN ('READY_FOR_PICKUP','REJECTED_BY_RESTAURANT')) OR
    (OLD.status='READY_FOR_PICKUP' AND NEW.status='dispatched') OR
    (OLD.status='confirmed' AND NEW.status IN ('preparing','cancelled')) OR
    (OLD.status='preparing' AND NEW.status IN ('ready','cancelled')) OR
    (OLD.status='ready' AND NEW.status IN ('dispatched','cancelled')) OR
    (OLD.status='dispatched' AND NEW.status='delivered')) THEN
    RAISE EXCEPTION 'invalid order status transition: % -> %',OLD.status,NEW.status USING ERRCODE='check_violation';
  END IF;
  RETURN NEW;
END; $$;
ALTER TABLE orders DROP COLUMN IF EXISTS delivered_at;
ALTER TABLE orders DROP COLUMN IF EXISTS picked_up_at;
ALTER TABLE orders DROP COLUMN IF EXISTS ready_at;
ALTER TABLE orders DROP COLUMN IF EXISTS courier_assigned_at;
ALTER TABLE orders DROP COLUMN IF EXISTS delivery_pin_hash;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_restaurant_status_check;
ALTER TABLE orders DROP COLUMN IF EXISTS restaurant_status;
