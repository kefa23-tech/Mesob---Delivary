# Mesob production deployment runbook

This runbook targets the repository's Docker Compose production stack. Run commands from the repository root on a Linux host with Docker Engine and Compose v2 installed.

## 1. Prepare the host and secrets

1. Point public DNS A/AAAA records for the customer and admin hostnames to this host. Allow inbound TCP 80/443 and outbound HTTPS for certificate issuance and payment providers.
2. Create `.env.production` in the repository root and restrict access (`chmod 600 .env.production`). Never commit it. Set:

   ```dotenv
   MESOB_DOMAIN=app.example.com
   ADMIN_DOMAIN=admin.example.com
   POSTGRES_DB=mesob
   POSTGRES_USER=mesob
   POSTGRES_PASSWORD=<unique-long-random-secret>
   GRAFANA_ADMIN_PASSWORD=<unique-long-random-secret>
   DATABASE_URL=postgres://mesob:<URL-encoded-password>@postgres:5432/mesob?sslmode=disable
   JWT_SECRET=<at-least-32-random-bytes>
   SENTRY_DSN=<backend-project-dsn>
   SENTRY_ENVIRONMENT=production
   NEXT_PUBLIC_SENTRY_DSN=<frontend-project-dsn>
   NEXT_PUBLIC_SENTRY_ENVIRONMENT=production
   CHAPA_SECRET_KEY=<provider-secret>
   CHAPA_WEBHOOK_SECRET=<provider-webhook-secret>
   TELEBIRR_API_URL=<provider-url>
   TELEBIRR_API_KEY=<provider-key>
   TELEBIRR_WEBHOOK_SECRET=<provider-webhook-secret>
   SMS_GATEWAY_URL=<configured-sms-adapter-url>
   WHATSAPP_GATEWAY_URL=<configured-whatsapp-adapter-url>
   ```

   `DATABASE_URL` is consumed inside Compose; `sslmode=disable` is appropriate only on the private Compose network. Set only the payment and messaging credentials for providers actually enabled in production. The frontend DSN is public by design; never put a Sentry auth token or backend secret in `NEXT_PUBLIC_*`.
3. Verify required values without printing secrets: `docker compose --env-file .env.production -f docker-compose.prod.yml config --quiet`.

## 2. Database and persistent services

1. The compose stack uses `postgis/postgis:16-3.4`, with a named `postgres_data` volume. Confirm available disk space and configure host-level encrypted backups before launch.
2. Redis enables AOF with `appendfsync everysec`, and stores data in the named `redis_data` volume. Protect the host and Docker network; Redis is not published to the host. Include the volume in backup and recovery procedures because it holds queues, sessions, and cache state.
3. Start the stack. The API waits for Postgres/Redis health and applies versioned migrations from `backend/migrations` before serving:

   ```sh
   docker compose --env-file .env.production -f docker-compose.prod.yml up -d --build
   docker compose --env-file .env.production -f docker-compose.prod.yml ps
   docker compose --env-file .env.production -f docker-compose.prod.yml logs --tail=100 api worker
   ```

   Confirm the migration log is clean before routing user traffic. For a separate migration review, inspect `docker compose ... logs api` and query `SELECT version, dirty FROM schema_migrations;` in the `mesob` database. A dirty migration is a stop condition; restore/repair it using the matching migration pair, then restart.

## 3. TLS and deployment

The included proxy is Caddy. It obtains and renews certificates automatically when both DNS names resolve here and ports 80/443 are reachable. Persist `caddy_data` and `caddy_config`; they contain certificate and proxy state. Review `deploy/Caddyfile` before launch. If operating an external Nginx instead, terminate TLS with certificates from your approved ACME flow, proxy both hostnames to the frontend containers and `/api/v1` to API, pass `Host` and `X-Forwarded-Proto`, and do not expose API/DB/Redis ports publicly.

Deploy updates with `docker compose --env-file .env.production -f docker-compose.prod.yml up -d --build`; frontend API URLs are build-time values, so rebuild after hostname changes. Keep a release commit/image tag and a tested Postgres backup for rollback. Roll back by deploying the prior image/revision and restoring database state only when that release's migration compatibility requires it.

## 4. Smoke checks

Run after rollout, substituting your public hostnames:

```sh
curl -fsS https://app.example.com/api/v1/health
curl -fsSI https://app.example.com/
curl -fsSI https://admin.example.com/
curl -fsS https://app.example.com/metrics | grep -E 'http_request_duration_seconds|mesob_ledger_balanced|mesob_active_sse_connections'
docker compose --env-file .env.production -f docker-compose.prod.yml exec redis redis-cli ping
docker compose --env-file .env.production -f docker-compose.prod.yml exec postgres pg_isready -U mesob -d mesob
docker compose --env-file .env.production -f docker-compose.prod.yml ps
```

Expect HTTPS without certificate warnings, health status 200, Redis `PONG`, healthy containers, and ledger gauge `mesob_ledger_balanced 1` after scraping. Exercise a real OTP flow, customer order creation, courier location update, and payment-provider sandbox webhook; verify the resulting API responses, worker logs, and provider dashboard. Do not use real charges for smoke testing.

## 5. Monitoring and launch acceptance

Grafana, Prometheus, and the Redis exporter run on the private Compose network. The dashboard and datasource are provisioned from `deploy/monitoring/`; Grafana binds only to host loopback, so use `ssh -L 3000:127.0.0.1:3000 <host>` and open `http://127.0.0.1:3000`. Do not publish Grafana, Prometheus, or Redis exporter ports to the public network. The Caddy routes intentionally do not expose `/metrics`; Prometheus scrapes API `/metrics` directly over the private network and Redis exporter metrics using `deploy/monitoring/prometheus.yml`. Alert on `mesob_ledger_balanced != 1`, rising DLQ counts, sustained 5xx rate, and missing scrape targets. Confirm Sentry events and sampled traces arrive from both backend and Next.js frontends. Establish on-call ownership, backup restore owner, payment reconciliation contact, and rollback decision maker before opening public traffic.

Sentry Go provides error capture and sampled performance traces; its Go profiler was removed from the SDK, so the backend exposes Go's built-in pprof handlers on loopback (`127.0.0.1:6060`) inside each API/worker container. Capture an on-demand CPU profile without publishing that port:

```sh
docker compose --env-file .env.production -f docker-compose.prod.yml exec api wget -O /tmp/cpu.pprof 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
docker compose --env-file .env.production -f docker-compose.prod.yml cp api:/tmp/cpu.pprof ./cpu.pprof
go tool pprof ./cpu.pprof
```

Remove the copied profile after analysis because it can contain sensitive stack and workload information.
