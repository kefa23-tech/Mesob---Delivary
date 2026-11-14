DROP TABLE IF EXISTS order_events;
DROP TABLE IF EXISTS payment_transactions;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check
    CHECK (payment_status IN ('pending', 'authorized', 'paid', 'failed', 'refunded'));
UPDATE orders SET payment_status='pending' WHERE payment_status='pending_payment';
UPDATE orders SET payment_status='failed' WHERE payment_status='payment_failed';
