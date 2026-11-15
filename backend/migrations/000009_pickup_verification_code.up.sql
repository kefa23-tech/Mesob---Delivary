ALTER TABLE orders ADD COLUMN pickup_code CHAR(6);
UPDATE orders SET pickup_code=lpad((floor(random()*1000000))::bigint::text,6,'0')
WHERE courier_id IS NOT NULL AND status IN ('COURIER_ASSIGNED','PICKED_UP');
