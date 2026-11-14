ALTER TABLE orders
    ADD COLUMN payment_method TEXT NOT NULL DEFAULT 'COD'
        CHECK (payment_method IN ('TELEBIRR', 'CHAPA', 'COD')),
    ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX orders_user_id_idempotency_key_idx
    ON orders(user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
