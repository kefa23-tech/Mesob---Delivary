-- COD account controls and courier assignment state.
ALTER TABLE users
    ADD COLUMN cod_suspended BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN cod_suspension_reason TEXT,
    ADD COLUMN courier_cod_limit_minor BIGINT NOT NULL DEFAULT 500000
        CHECK (courier_cod_limit_minor >= 0);

ALTER TABLE orders
    ADD COLUMN courier_id UUID REFERENCES users(id);

ALTER TABLE courier_cash_balances
    ADD COLUMN reserved_minor BIGINT NOT NULL DEFAULT 0 CHECK (reserved_minor >= 0),
    ADD CONSTRAINT courier_cash_balance_nonnegative CHECK (balance_minor >= 0);

-- Preserve the original event-style table while replacing it with an immutable
-- journal whose lines use signed santim (positive debit, negative credit).
ALTER TABLE ledger_entries RENAME TO legacy_ledger_entries;
DROP INDEX IF EXISTS ledger_entries_transaction_id_idx;
DROP INDEX IF EXISTS ledger_entries_account_code_idx;

CREATE TABLE ledger_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_key TEXT NOT NULL UNIQUE,
    account_type TEXT NOT NULL CHECK (account_type IN (
        'system_clearing', 'customer_clearing', 'platform_revenue',
        'restaurant_payable', 'courier_payable', 'courier_cash_receivable',
        'delivery_fee_pool'
    )),
    owner_user_id UUID REFERENCES users(id),
    owner_restaurant_id UUID REFERENCES restaurants(id),
    currency CHAR(3) NOT NULL DEFAULT 'ETB',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (account_type = 'restaurant_payable' AND owner_restaurant_id IS NOT NULL AND owner_user_id IS NULL)
        OR (account_type IN ('courier_payable', 'courier_cash_receivable') AND owner_user_id IS NOT NULL AND owner_restaurant_id IS NULL)
        OR (account_type IN ('system_clearing', 'customer_clearing', 'platform_revenue', 'delivery_fee_pool') AND owner_user_id IS NULL AND owner_restaurant_id IS NULL)
    )
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_xid XID8 NOT NULL DEFAULT pg_current_xact_id(),
    source_key TEXT NOT NULL UNIQUE,
    order_id UUID REFERENCES orders(id),
    entry_type TEXT NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'ETB',
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_order_id_idx ON ledger_entries(order_id, created_at);

CREATE TABLE ledger_lines (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entry_id UUID NOT NULL REFERENCES ledger_entries(id),
    line_number SMALLINT NOT NULL CHECK (line_number > 0),
    account_id UUID NOT NULL REFERENCES ledger_accounts(id),
    amount_minor BIGINT NOT NULL CHECK (amount_minor <> 0),
    UNIQUE (entry_id, line_number)
);
CREATE INDEX ledger_lines_account_id_idx ON ledger_lines(account_id, entry_id);

INSERT INTO ledger_accounts(account_key, account_type) VALUES
    ('system_clearing', 'system_clearing'),
    ('customer_clearing', 'customer_clearing'),
    ('platform_revenue', 'platform_revenue'),
    ('delivery_fee_pool', 'delivery_fee_pool');

CREATE FUNCTION assert_ledger_entry_balanced(p_entry_id UUID) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE
    line_count BIGINT;
    signed_total NUMERIC;
BEGIN
    SELECT count(*), COALESCE(sum(amount_minor), 0)
      INTO line_count, signed_total
      FROM ledger_lines WHERE entry_id = p_entry_id;
    IF line_count < 2 OR signed_total <> 0 THEN
        RAISE EXCEPTION 'ledger entry % is not balanced (lines %, signed total %)', p_entry_id, line_count, signed_total
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE FUNCTION check_ledger_entry_balance_from_entry() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_ledger_entry_balanced(NEW.id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION check_ledger_entry_balance_from_line() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_ledger_entry_balanced(NEW.entry_id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER ledger_entry_balanced_after_entry
AFTER INSERT ON ledger_entries DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_ledger_entry_balance_from_entry();
CREATE CONSTRAINT TRIGGER ledger_entry_balanced_after_line
AFTER INSERT ON ledger_lines DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_ledger_entry_balance_from_line();

CREATE FUNCTION reject_ledger_mutation() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger history is immutable' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER ledger_entries_immutable
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER ledger_lines_immutable
BEFORE UPDATE OR DELETE ON ledger_lines
FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION reject_late_ledger_line() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE entry_xid XID8;
BEGIN
    SELECT created_xid INTO entry_xid FROM ledger_entries WHERE id = NEW.entry_id;
    IF entry_xid IS NULL OR entry_xid <> pg_current_xact_id() THEN
        RAISE EXCEPTION 'ledger entry is immutable after its creating transaction' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER ledger_lines_same_transaction
BEFORE INSERT ON ledger_lines
FOR EACH ROW EXECUTE FUNCTION reject_late_ledger_line();

CREATE FUNCTION ensure_ledger_account(
    p_key TEXT, p_type TEXT, p_user_id UUID DEFAULT NULL, p_restaurant_id UUID DEFAULT NULL
) RETURNS UUID LANGUAGE plpgsql AS $$
DECLARE account_id UUID;
BEGIN
    INSERT INTO ledger_accounts(account_key, account_type, owner_user_id, owner_restaurant_id)
    VALUES (p_key, p_type, p_user_id, p_restaurant_id)
    ON CONFLICT (account_key) DO UPDATE SET account_key = EXCLUDED.account_key
    RETURNING id INTO account_id;
    RETURN account_id;
END;
$$;

-- Delivery completion is the point at which COD cash is actually collected.
-- The trigger also moves online delivery earnings from the fee pool to the courier.
CREATE FUNCTION post_order_delivery_ledger() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    v_entry_id UUID;
    v_restaurant_account UUID;
    v_courier_payable UUID;
    v_courier_cash UUID;
    v_restaurant_minor BIGINT;
    v_platform_minor BIGINT;
    v_delivery_minor BIGINT;
BEGIN
    IF NEW.courier_id IS NULL THEN
        RAISE EXCEPTION 'cannot complete order % without an assigned courier', NEW.id USING ERRCODE = '23514';
    END IF;

    INSERT INTO ledger_entries(source_key, order_id, entry_type, currency, description)
    VALUES ('order:' || NEW.id::text || ':delivered', NEW.id,
            CASE WHEN NEW.payment_method = 'COD' THEN 'cod_delivery' ELSE 'delivery_completion' END,
            NEW.currency, 'Order delivery settlement')
    ON CONFLICT (source_key) DO NOTHING
    RETURNING id INTO v_entry_id;
    IF v_entry_id IS NULL THEN
        RETURN NEW;
    END IF;

    v_restaurant_minor := NEW.subtotal_minor - NEW.commission_minor;
    v_platform_minor := NEW.commission_minor;
    v_delivery_minor := NEW.delivery_fee_minor;
    v_restaurant_account := ensure_ledger_account(
        'restaurant_payable:' || NEW.restaurant_id::text, 'restaurant_payable', NULL, NEW.restaurant_id);
    v_courier_payable := ensure_ledger_account(
        'courier_payable:' || NEW.courier_id::text, 'courier_payable', NEW.courier_id, NULL);

    IF NEW.payment_method = 'COD' THEN
        v_courier_cash := ensure_ledger_account(
            'courier_cash_receivable:' || NEW.courier_id::text, 'courier_cash_receivable', NEW.courier_id, NULL);
        INSERT INTO ledger_lines(entry_id, line_number, account_id, amount_minor)
        VALUES (v_entry_id, 1, v_courier_cash, NEW.total_minor);
        IF v_restaurant_minor > 0 THEN
            INSERT INTO ledger_lines(entry_id, line_number, account_id, amount_minor)
            VALUES (v_entry_id, 2, v_restaurant_account, -v_restaurant_minor);
        END IF;
        IF v_platform_minor > 0 THEN
            INSERT INTO ledger_lines(entry_id, line_number, account_id, amount_minor)
            VALUES (v_entry_id, 3, ensure_ledger_account('platform_revenue', 'platform_revenue'), -v_platform_minor);
        END IF;
        IF v_delivery_minor > 0 THEN
            INSERT INTO ledger_lines(entry_id, line_number, account_id, amount_minor)
            VALUES (v_entry_id, 4, v_courier_payable, -v_delivery_minor);
        END IF;
        INSERT INTO courier_cash_balances(courier_id, balance_minor, reserved_minor, currency, updated_at)
        VALUES (NEW.courier_id, NEW.total_minor, 0, NEW.currency, now())
        ON CONFLICT (courier_id) DO UPDATE
            SET balance_minor = courier_cash_balances.balance_minor + EXCLUDED.balance_minor,
                reserved_minor = GREATEST(0, courier_cash_balances.reserved_minor - NEW.total_minor),
                updated_at = now();
        UPDATE orders SET payment_status = 'paid' WHERE id = NEW.id AND payment_method = 'COD';
    ELSE
        IF v_delivery_minor > 0 THEN
            INSERT INTO ledger_lines(entry_id, line_number, account_id, amount_minor)
            VALUES (v_entry_id, 1, ensure_ledger_account('delivery_fee_pool', 'delivery_fee_pool'), v_delivery_minor),
                   (v_entry_id, 2, v_courier_payable, -v_delivery_minor);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER orders_post_delivery_ledger
AFTER UPDATE OF status ON orders
FOR EACH ROW WHEN (NEW.status = 'delivered' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION post_order_delivery_ledger();

CREATE FUNCTION reserve_courier_cod_capacity() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    v_role TEXT;
    v_status TEXT;
    v_limit BIGINT;
    v_balance BIGINT;
    v_reserved BIGINT;
BEGIN
    IF NEW.courier_id IS NULL OR NEW.courier_id IS NOT DISTINCT FROM OLD.courier_id THEN
        RETURN NEW;
    END IF;
    IF OLD.courier_id IS NOT NULL THEN
        RAISE EXCEPTION 'order % already has a courier assignment', NEW.id USING ERRCODE = '23514';
    END IF;
    SELECT role, status, courier_cod_limit_minor
      INTO v_role, v_status, v_limit
      FROM users WHERE id = NEW.courier_id FOR UPDATE;
    IF NOT FOUND OR v_role <> 'courier' OR v_status <> 'active' THEN
        RAISE EXCEPTION 'courier is not active and eligible' USING ERRCODE = '23514';
    END IF;
    IF NEW.payment_method = 'COD' THEN
        INSERT INTO courier_cash_balances(courier_id, balance_minor, reserved_minor, currency)
        VALUES (NEW.courier_id, 0, 0, NEW.currency)
        ON CONFLICT (courier_id) DO NOTHING;
        SELECT balance_minor, reserved_minor INTO v_balance, v_reserved
          FROM courier_cash_balances WHERE courier_id = NEW.courier_id FOR UPDATE;
        IF v_balance + v_reserved + NEW.total_minor > v_limit THEN
            RAISE EXCEPTION 'courier COD cash limit exceeded' USING ERRCODE = '23514';
        END IF;
        UPDATE courier_cash_balances
           SET reserved_minor = reserved_minor + NEW.total_minor, updated_at = now()
         WHERE courier_id = NEW.courier_id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER orders_reserve_courier_cod_capacity
BEFORE UPDATE OF courier_id ON orders
FOR EACH ROW EXECUTE FUNCTION reserve_courier_cod_capacity();

CREATE FUNCTION release_cod_reservation_on_cancel() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.payment_method = 'COD' AND OLD.courier_id IS NOT NULL
       AND NEW.status = 'cancelled' AND OLD.status IS DISTINCT FROM NEW.status THEN
        UPDATE courier_cash_balances
           SET reserved_minor = GREATEST(0, reserved_minor - OLD.total_minor), updated_at = now()
         WHERE courier_id = OLD.courier_id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER orders_release_cod_reservation
AFTER UPDATE OF status ON orders
FOR EACH ROW WHEN (NEW.status = 'cancelled' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION release_cod_reservation_on_cancel();
