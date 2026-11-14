DROP INDEX IF EXISTS orders_user_id_idempotency_key_idx;
ALTER TABLE orders DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE orders DROP COLUMN IF EXISTS payment_method;
