ALTER TABLE orders
  ADD COLUMN restaurant_status TEXT,
  ADD COLUMN delivery_pin_hash BYTEA,
  ADD COLUMN courier_assigned_at TIMESTAMPTZ,
  ADD COLUMN ready_at TIMESTAMPTZ,
  ADD COLUMN picked_up_at TIMESTAMPTZ,
  ADD COLUMN delivered_at TIMESTAMPTZ;
ALTER TABLE orders ADD CONSTRAINT orders_restaurant_status_check CHECK (restaurant_status IS NULL OR restaurant_status IN ('ACCEPTED','PREPARING','READY_FOR_PICKUP'));

CREATE TABLE courier_presence (
  courier_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  online BOOLEAN NOT NULL DEFAULT false,
  vehicle_type TEXT NOT NULL CHECK (vehicle_type IN ('bicycle','motorbike','car')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX courier_presence_online_idx ON courier_presence(online) WHERE online=true;

CREATE FUNCTION enforce_restaurant_fulfillment_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.restaurant_status IS NOT DISTINCT FROM NEW.restaurant_status THEN RETURN NEW; END IF;
  IF NOT ((OLD.restaurant_status IS NULL AND NEW.restaurant_status='ACCEPTED' AND OLD.status IN ('PAID','CONFIRMED_COD')) OR
          (OLD.restaurant_status='ACCEPTED' AND NEW.restaurant_status='PREPARING') OR
          (OLD.restaurant_status='PREPARING' AND NEW.restaurant_status='READY_FOR_PICKUP')) THEN
    RAISE EXCEPTION 'invalid restaurant fulfillment transition: % -> %',OLD.restaurant_status,NEW.restaurant_status USING ERRCODE='check_violation';
  END IF;
  RETURN NEW;
END; $$;
CREATE TRIGGER orders_restaurant_fulfillment_transition BEFORE UPDATE OF restaurant_status ON orders
FOR EACH ROW EXECUTE FUNCTION enforce_restaurant_fulfillment_transition();

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK(status IN (
  'pending','confirmed','preparing','ready','dispatched','delivered','cancelled',
  'PAID','CONFIRMED_COD','ACCEPTED','PREPARING','READY_FOR_PICKUP','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT',
  'COURIER_ASSIGNED','PICKED_UP','DELIVERED'
));

CREATE OR REPLACE FUNCTION enforce_order_lifecycle_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = NEW.status THEN RETURN NEW; END IF;
  IF NOT (
    (OLD.status IN ('pending','pending_payment') AND NEW.status IN ('confirmed','PAID','CONFIRMED_COD','cancelled')) OR
    (OLD.status IN ('PAID','CONFIRMED_COD') AND NEW.status IN ('ACCEPTED','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT')) OR
    (OLD.status = 'ACCEPTED' AND NEW.status IN ('PREPARING','COURIER_ASSIGNED','REJECTED_BY_RESTAURANT')) OR
    (OLD.status = 'PREPARING' AND NEW.status IN ('READY_FOR_PICKUP','COURIER_ASSIGNED','REJECTED_BY_RESTAURANT')) OR
    (OLD.status = 'READY_FOR_PICKUP' AND NEW.status IN ('COURIER_ASSIGNED','dispatched')) OR
    (OLD.status = 'COURIER_ASSIGNED' AND NEW.status IN ('READY_FOR_PICKUP','PICKED_UP','REJECTED_BY_RESTAURANT','CANCELLED_TIMEOUT')) OR
    (OLD.status = 'PICKED_UP' AND NEW.status = 'DELIVERED') OR
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

DROP TRIGGER IF EXISTS orders_post_delivery_ledger ON orders;
CREATE TRIGGER orders_post_delivery_ledger
AFTER UPDATE OF status ON orders
FOR EACH ROW WHEN (NEW.status IN ('delivered','DELIVERED') AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION post_order_delivery_ledger();
