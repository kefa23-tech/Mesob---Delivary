CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    phone TEXT NOT NULL UNIQUE,
    email TEXT UNIQUE,
    password_hash TEXT,
    role TEXT NOT NULL DEFAULT 'customer' CHECK (role IN ('customer', 'courier', 'restaurant_owner', 'admin')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'suspended', 'deleted')),
    phone_verified_at TIMESTAMPTZ,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    lat NUMERIC(9,6) NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng NUMERIC(9,6) NOT NULL CHECK (lng BETWEEN -180 AND 180),
    line1 TEXT NOT NULL,
    landmark_name TEXT,
    gate_floor_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX addresses_user_id_idx ON addresses(user_id);

CREATE TABLE restaurants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    lat NUMERIC(9,6) NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng NUMERIC(9,6) NOT NULL CHECK (lng BETWEEN -180 AND 180),
    open_hours JSONB NOT NULL DEFAULT '{}'::jsonb,
    delivery_radius_km NUMERIC(6,2) NOT NULL DEFAULT 5 CHECK (delivery_radius_km > 0),
    is_open BOOLEAN NOT NULL DEFAULT false,
    commission_pct NUMERIC(5,2) NOT NULL DEFAULT 0 CHECK (commission_pct BETWEEN 0 AND 100),
    eco_packaging_certified BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX restaurants_owner_id_idx ON restaurants(owner_id);

CREATE TABLE menu_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    restaurant_id UUID NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (restaurant_id, name)
);

CREATE TABLE menu_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID NOT NULL REFERENCES menu_categories(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    price_minor BIGINT NOT NULL CHECK (price_minor >= 0),
    stock_limit INTEGER CHECK (stock_limit IS NULL OR stock_limit >= 0),
    stock_remaining INTEGER CHECK (stock_remaining IS NULL OR stock_remaining >= 0),
    is_available BOOLEAN NOT NULL DEFAULT true,
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX menu_categories_restaurant_id_idx ON menu_categories(restaurant_id);
CREATE INDEX menu_items_category_id_idx ON menu_items(category_id);

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    restaurant_id UUID NOT NULL REFERENCES restaurants(id),
    delivery_address_id UUID NOT NULL REFERENCES addresses(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'preparing', 'ready', 'dispatched', 'delivered', 'cancelled')),
    payment_status TEXT NOT NULL DEFAULT 'pending' CHECK (payment_status IN ('pending', 'authorized', 'paid', 'failed', 'refunded')),
    subtotal_minor BIGINT NOT NULL DEFAULT 0 CHECK (subtotal_minor >= 0),
    delivery_fee_minor BIGINT NOT NULL DEFAULT 0 CHECK (delivery_fee_minor >= 0),
    commission_minor BIGINT NOT NULL DEFAULT 0 CHECK (commission_minor >= 0),
    total_minor BIGINT NOT NULL DEFAULT 0 CHECK (total_minor >= 0),
    currency CHAR(3) NOT NULL DEFAULT 'ETB',
    notes TEXT,
    placed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX orders_user_id_created_at_idx ON orders(user_id, created_at DESC);
CREATE INDEX orders_restaurant_id_status_idx ON orders(restaurant_id, status);

CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    menu_item_id UUID REFERENCES menu_items(id),
    item_name TEXT NOT NULL,
    unit_price_minor BIGINT NOT NULL CHECK (unit_price_minor >= 0),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    line_total_minor BIGINT NOT NULL CHECK (line_total_minor >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_items_order_id_idx ON order_items(order_id);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL,
    account_code TEXT NOT NULL,
    order_id UUID REFERENCES orders(id),
    user_id UUID REFERENCES users(id),
    amount_minor BIGINT NOT NULL CHECK (amount_minor <> 0),
    currency CHAR(3) NOT NULL DEFAULT 'ETB',
    entry_type TEXT NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_transaction_id_idx ON ledger_entries(transaction_id);
CREATE INDEX ledger_entries_account_code_idx ON ledger_entries(account_code, created_at);

CREATE TABLE courier_cash_balances (
    courier_id UUID PRIMARY KEY REFERENCES users(id),
    balance_minor BIGINT NOT NULL DEFAULT 0,
    currency CHAR(3) NOT NULL DEFAULT 'ETB',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dead_letter_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    error_message TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'replaying', 'resolved', 'discarded')),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX dead_letter_jobs_status_available_at_idx ON dead_letter_jobs(status, available_at);
