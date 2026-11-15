FROM caddy:2-builder-alpine AS build
RUN xcaddy build --with github.com/ueffel/caddy-brotli

FROM caddy:2-alpine
COPY --from=build /usr/bin/caddy /usr/bin/caddy
