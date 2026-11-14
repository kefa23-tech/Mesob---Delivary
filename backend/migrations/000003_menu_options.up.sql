ALTER TABLE menu_items
    ADD COLUMN image_url TEXT,
    ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

CREATE TABLE menu_option_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    min_select INTEGER NOT NULL DEFAULT 0 CHECK (min_select >= 0),
    max_select INTEGER NOT NULL DEFAULT 1 CHECK (max_select >= min_select),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (menu_item_id, name)
);

CREATE TABLE menu_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    option_group_id UUID NOT NULL REFERENCES menu_option_groups(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    price_delta_minor BIGINT NOT NULL DEFAULT 0,
    is_available BOOLEAN NOT NULL DEFAULT true,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (option_group_id, name)
);

CREATE INDEX menu_option_groups_item_idx ON menu_option_groups(menu_item_id, sort_order);
CREATE INDEX menu_options_group_idx ON menu_options(option_group_id, sort_order);
