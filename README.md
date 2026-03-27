# WordBit Advanced Backend

Production-minded Go backend for a vocabulary learning system. It owns daily pool generation, SRS scheduling, Gemini-driven candidate generation, deduplication, weak-word resurfacing, and learning-event persistence.

## Stack

- Go `chi` HTTP API
- PostgreSQL with SQL migrations
- `pgxpool` repositories
- Google Gemini via direct REST client
- Structured JSON logging with `slog`
- In-process cron scheduler for weakness refresh only
- Docker and docker-compose for local/VPS deployment

## Folder Structure

```text
backend/
  cmd/api
  docs/
  internal/
    auth
    config
    database
    domain
    http
    integrations/gemini
    repository/postgres
    scheduler
    service
  migrations/
  .env.example
  Dockerfile
  Makefile
  README.md
  docker-compose.yml
  go.mod
  go.sum
```

## Local Setup

1. Copy `.env.example` to `.env`.
2. Set `GEMINI_API_KEY` or `GEMINI_API_KEY_FILE`.
3. Set `GEMINI_MODEL` and optionally `GEMINI_MODEL_2` / `GEMINI_MODEL_3` if you want automatic fallback rotation.
4. Set `GEMINI_RPM_LIMIT` / `GEMINI_RPD_LIMIT` to match your current Gemini tier if you want the backend to skip locally exhausted models before making live requests.
5. For local auth, keep `DEV_AUTH_BYPASS=true`.
6. Start dependencies with `docker compose up -d postgres` or run the full stack with `docker compose up --build`.

## Run Locally

```bash
make migrate-up
make run
```

Default server address: `http://localhost:8080`

Health check:

```bash
curl http://localhost:8080/healthz
```

## Tests

```bash
make test
make test-integration
```

`make test-integration` starts a disposable PostgreSQL Docker container and verifies migrations plus repository round-trips.

## API Contract

- OpenAPI: [docs/openapi.yaml](docs/openapi.yaml)
- Human summary: [docs/api.md](docs/api.md)

## Docker / VPS Deployment Notes

- Build with `docker build -t wordbit-backend .`
- Run with environment variables injected by your VPS orchestrator or `.env`
- Set `DEV_AUTH_BYPASS=false` and configure `AUTH_JWKS_URL`, `AUTH_ISSUER`, and `AUTH_AUDIENCE`
- Set `GEMINI_API_KEY` through secrets or environment injection
- Use `GEMINI_MODEL`, `GEMINI_MODEL_2`, and `GEMINI_MODEL_3` to configure fallback model rotation
- Use `GEMINI_RPM_LIMIT` and `GEMINI_RPD_LIMIT` to match the request quotas of your current Gemini plan
- `AUTO_MIGRATE=true` is enabled by default for simple VPS deployment; disable it if you prefer an explicit migration step
- Expose only the backend port and place a reverse proxy in front if you need TLS termination

## Remaining TODOs

- Tune the confusable-group rule set with product data
- Add richer LLM rejection analytics and prompt versioning
- Add pagination/filtering for admin LLM run inspection
- Add observability hooks for metrics/tracing if required by production
# wordbit-advanced
