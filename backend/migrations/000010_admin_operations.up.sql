ALTER TABLE restaurants
  ADD COLUMN tin TEXT,
  ADD COLUMN business_license_url TEXT,
  ADD COLUMN kyc_review_notes TEXT,
  ADD COLUMN kyc_reviewed_at TIMESTAMPTZ;

ALTER TABLE users
  ADD COLUMN national_id TEXT,
  ADD COLUMN driver_license_url TEXT,
  ADD COLUMN courier_vehicle_info JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN kyc_review_notes TEXT,
  ADD COLUMN kyc_reviewed_at TIMESTAMPTZ;

CREATE TABLE admin_audit_log (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  admin_user_id UUID NOT NULL REFERENCES users(id),
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  details JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_log_created_at_idx ON admin_audit_log(created_at DESC);
CREATE INDEX admin_audit_log_entity_idx ON admin_audit_log(entity_type, entity_id, created_at DESC);

CREATE TABLE order_disputes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id),
  opened_by UUID REFERENCES users(id),
  reason TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
  resolution TEXT,
  reviewer_notes TEXT,
  resolved_by UUID REFERENCES users(id),
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_disputes_open_idx ON order_disputes(status, created_at DESC);

-- A verified administrator may cancel an order after taking a row lock and
-- recording a dispute decision. All ordinary callers still follow lifecycle rules.
CREATE OR REPLACE FUNCTION enforce_order_lifecycle_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = NEW.status THEN RETURN NEW; END IF;
  IF current_setting('app.admin_operation', true) = 'true' AND NEW.status = 'cancelled' THEN
    RETURN NEW;
  END IF;
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
