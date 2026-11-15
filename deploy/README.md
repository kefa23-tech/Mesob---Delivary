# Production Compose deployment

Use Docker Compose v2 from the repository root. Point public DNS for both hostnames at the server first; Caddy obtains and renews TLS certificates automatically.

Required environment values are `MESOB_DOMAIN`, `ADMIN_DOMAIN`, `POSTGRES_PASSWORD`, `DATABASE_URL`, and a high-entropy `JWT_SECRET`. `DATABASE_URL` must address Compose service `postgres` (for example, `postgres://mesob:<password>@postgres:5432/mesob?sslmode=disable`). Configure the payment, SMS, and WhatsApp credentials as needed. Keep these values in an untracked production env file and restrict its permissions.

Build and start with `docker compose --env-file .env.prod -f docker-compose.prod.yml up -d --build`. Only Caddy publishes host ports; PostgreSQL, Redis, API, worker, and Next.js services remain on the internal Compose network. PostgreSQL and Redis data and Caddy's certificate state use named volumes. The API applies database migrations at startup; the worker waits for the API health check before starting.

The frontend API URLs are embedded at build time, so rebuild the frontend images after changing either hostname. `PROCESS_ROLE=api` and `PROCESS_ROLE=worker` select separate statically linked binaries in the backend image.
