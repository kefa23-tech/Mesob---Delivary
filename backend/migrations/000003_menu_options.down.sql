DROP INDEX IF EXISTS menu_options_group_idx;
DROP INDEX IF EXISTS menu_option_groups_item_idx;
DROP TABLE IF EXISTS menu_options;
DROP TABLE IF EXISTS menu_option_groups;
ALTER TABLE menu_items
    DROP COLUMN IF EXISTS sort_order,
    DROP COLUMN IF EXISTS image_url;
