DROP TABLE IF EXISTS order_disputes;
DROP TABLE IF EXISTS admin_audit_log;
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
  ) THEN RAISE EXCEPTION 'invalid order status transition: % -> %', OLD.status, NEW.status USING ERRCODE = 'check_violation'; END IF;
  RETURN NEW;
END;
$$;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_reviewed_at, DROP COLUMN IF EXISTS kyc_review_notes,
  DROP COLUMN IF EXISTS courier_vehicle_info, DROP COLUMN IF EXISTS driver_license_url, DROP COLUMN IF EXISTS national_id;
ALTER TABLE restaurants DROP COLUMN IF EXISTS kyc_reviewed_at, DROP COLUMN IF EXISTS kyc_review_notes,
  DROP COLUMN IF EXISTS business_license_url, DROP COLUMN IF EXISTS tin;
