# k6 load tests

Install k6 and provide a JSON array of bearer tokens in a local file (one token per test user/courier). Do not commit production credentials.

Common settings: `API_BASE_URL` (defaults to `http://localhost:8080/api/v1`) and `AUTH_TOKENS_FILE`.

`flash_sale_inventory.js` additionally requires `ITEM_ID`, `ADDRESS_ID`, and `DATABASE_URL`; the selected item must have exactly five units available and each of 200 tokens must belong to a distinct customer with a usable address. It asserts exactly five HTTP 201 checkouts and 195 stock conflicts (409/422). `make test-load` checks persisted `stock_remaining` equals zero through `psql`.

`courier_dispatch_race.js` requires `ORDER_ID` for an active offer and 50 online eligible courier tokens. It asserts exactly one 200 and 49 409 responses.

`peak_discovery_and_tracking.js` requires `LAT` and `LNG`. Tune `SEARCH_VUS`, `GPS_VUS`, and `DURATION` as needed. It enforces p95 below 150 ms and zero failed requests.
