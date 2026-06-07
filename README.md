# crashtest_api

A small Go service that proxies requests to upstream HTTP APIs registered in a
Postgres-backed catalog. Clients hit `/` with an `api-id` query parameter and
the request is forwarded to the matching upstream's `base_url`.

## Layout

```
cmd/crashtest_api/        main entry point
internal/api/         CRUD HTTP handlers for the API catalog
internal/config/      env-driven configuration
internal/db/          Postgres store + startup initialization/migrations
internal/migrations/  embedded SQL migrations
internal/proxy/       reverse-proxy handler
```

## Configuration

| Env var        | Required | Default | Description                                                          |
|----------------|----------|---------|----------------------------------------------------------------------|
| `DATABASE_URL` | yes      | —       | Postgres URL, e.g. `postgres://user:pass@host:5432/dbname?sslmode=disable`. |
| `LISTEN_ADDR`  | no       | `:8080` | Address the HTTP server binds to.                                    |

## Database initialization

On startup the service:

1. Connects to the `postgres` maintenance database using the credentials in
   `DATABASE_URL` and creates the target database if it does not exist. The
   connecting user must have the `CREATEDB` privilege the first time.
2. Applies any pending migrations from `internal/migrations/*.sql` (embedded
   into the binary) in lexical order, recording applied versions in a
   `schema_migrations(version, applied_at)` table. Reruns are no-ops.

## Running

### Locally against a host Postgres

```sh
export DATABASE_URL='postgres://you@localhost:5432/middleman?sslmode=disable'
go run ./cmd/crashtest_api
```

### Docker (self-contained stack)

```sh
docker compose up --build
```

The compose file in this directory spins up a Postgres container alongside the
app. The root `docker-compose.yml` one level up instead points the app at the
Postgres running on the host.

## HTTP API

### Proxy

```
ANY  /?api-id=<id>[&...]
```

Looks up the API record with the given `id` (must be `enabled = true`), then
forwards the request to its `base_url`. The `api-id` query parameter is stripped
before forwarding. `X-Forwarded-*` headers are set.

### Catalog

All endpoints accept and return JSON. Errors use `{"error": "<message>"}`.

| Method | Path           | Body                                                   | Response                                |
|--------|----------------|--------------------------------------------------------|-----------------------------------------|
| POST   | `/apis`        | `{id, name, base_url, description?, enabled?}`         | `201` with the created record           |
| GET    | `/apis`        | —                                                      | `200` `{ "apis": [...] }`               |
| GET    | `/apis/{id}`   | —                                                      | `200` with the record, `404` if missing |
| PUT    | `/apis/{id}`   | any subset of `{name, base_url, description, enabled}` | `200` with the updated record           |
| DELETE | `/apis/{id}`   | —                                                      | `204` on success, `404` if missing      |

`description` accepts an explicit `null` on `PUT` to clear it. `base_url` must
use `http` or `https` and include a host.

### Health

```
GET /healthz   -> 200
```

## Schema

See `internal/migrations/`. The `apis` table holds the catalog; `id` is the
client-facing identifier passed as `api-id`.
