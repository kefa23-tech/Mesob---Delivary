.PHONY: test-e2e test-load test-unit test-architecture test-openapi

test-e2e:
	cd backend && go test -tags=e2e ./tests/e2e/...

test-load:
	command -v k6 >/dev/null || { echo "k6 is required (see loadtests/k6/README.md)"; exit 1; }
	: "$${AUTH_TOKENS_FILE:?set AUTH_TOKENS_FILE}" "$${DATABASE_URL:?set DATABASE_URL}" "$${ITEM_ID:?set ITEM_ID}"
	k6 run loadtests/k6/flash_sale_inventory.js
	stock=$$(psql "$$DATABASE_URL" -v item_id="$$ITEM_ID" -v ON_ERROR_STOP=1 -Atc "SELECT stock_remaining FROM menu_items WHERE id = :'item_id'::uuid"); test "$$stock" = 0 || { echo "expected stock_remaining=0, got $$stock"; exit 1; }
	k6 run loadtests/k6/courier_dispatch_race.js
	k6 run loadtests/k6/peak_discovery_and_tracking.js

test-unit:
	cd backend && go test ./...

test-architecture:
	cd backend && golangci-lint run ./...

test-openapi:
	cd api && npm run check
