# URL Shortener Service

A Go-based URL shortener API service for internal apps. Provides short link creation, custom aliases, link expiration, QR code generation, OG metadata proxying, and redirect handling.

## Tech Stack

- **Language:** Go
- **Router:** [Chi](https://github.com/go-chi/chi)
- **Database:** PostgreSQL (persistent storage)
- **Cache:** Redis (hot URL redirect cache)
- **Short codes:** Base62 encoding from a PostgreSQL sequence
- **Auth:** API key per app (SHA-256 hashed)
- **QR:** [skip2/go-qrcode](https://github.com/skip2/go-qrcode)
- **OG scraping:** [goquery](https://github.com/PuerkitoBio/goquery)

## Quick Start

### Docker Compose (recommended)

```bash
# Clone and start all services
cp .env.example .env
# Edit .env — set ADMIN_SECRET to a strong value

docker compose up --build
```

This starts the Go app on `:8080`, PostgreSQL on `:5432`, and Redis on `:6379`. Migrations run automatically.

### Create your first API key

```bash
curl -s -X POST http://localhost:8080/admin/api-keys \
  -H "Authorization: Bearer change-me-to-a-strong-secret" \
  -H "Content-Type: application/json" \
  -d '{"app_name": "my-app"}' | jq
```

Save the `api_key` from the response — it is only shown once.

### Shorten a URL

```bash
curl -s -X POST http://localhost:8080/api/v1/links \
  -H "X-API-Key: <your-api-key>" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/very/long/path"}' | jq
```

### Visit the short link

```bash
curl -L http://localhost:8080/2Bi
```

## API Reference

### Authentication

- **Admin routes** (`/admin/*`): `Authorization: Bearer <ADMIN_SECRET>` header
- **API routes** (`/api/v1/*`): `X-API-Key: <key>` header
- **Redirect** (`/{code}`): No auth required

---

### Admin Endpoints

#### Create API Key

```
POST /admin/api-keys
```

```json
{
  "app_name": "my-app"
}
```

**Response (201):**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "api_key": "a1b2c3d4e5f6...",
  "app_name": "my-app",
  "created_at": "2025-02-22T10:00:00Z"
}
```

> The `api_key` field is the raw key. It is only returned once — store it securely.

#### List API Keys

```
GET /admin/api-keys
```

**Response (200):**

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "app_name": "my-app",
    "is_active": true,
    "created_at": "2025-02-22T10:00:00Z",
    "updated_at": "2025-02-22T10:00:00Z"
  }
]
```

#### Deactivate API Key

```
DELETE /admin/api-keys/{id}
```

**Response:** `204 No Content`

---

### Link Endpoints

#### Create Short URL

```
POST /api/v1/links
```

```json
{
  "url": "https://example.com/very/long/path",
  "alias": "my-brand",
  "expires_in": 3600
}
```

| Field        | Type   | Required | Description                           |
|--------------|--------|----------|---------------------------------------|
| `url`        | string | yes      | The original URL to shorten           |
| `alias`      | string | no       | Custom short code (e.g. `my-brand`)   |
| `expires_in` | int    | no       | TTL in seconds, null = permanent      |

**Response (201):**

```json
{
  "code": "2BjL",
  "short_url": "http://localhost:8080/2BjL",
  "original_url": "https://example.com/very/long/path",
  "expires_at": "2025-03-01T12:00:00Z",
  "qr_url": "/api/v1/links/2BjL/qr",
  "og": {
    "title": "Example Page Title",
    "description": "A description from the original page",
    "image": "https://example.com/og-image.jpg",
    "site_name": "Example"
  },
  "created_at": "2025-02-22T10:00:00Z"
}
```

#### Get Link Info

```
GET /api/v1/links/{code}
```

**Response (200):**

```json
{
  "code": "2BjL",
  "short_url": "http://localhost:8080/2BjL",
  "original_url": "https://example.com/very/long/path",
  "click_count": 142,
  "is_alias": false,
  "expires_at": "2025-03-01T12:00:00Z",
  "created_at": "2025-02-22T10:00:00Z"
}
```

#### List Links

```
GET /api/v1/links?page=1&limit=20
```

Returns paginated links scoped to the authenticated API key.

#### Delete Link

```
DELETE /api/v1/links/{code}
```

**Response:** `204 No Content`

#### Get QR Code

```
GET /api/v1/links/{code}/qr?size=256
```

Returns a `image/png` QR code. Size in pixels (default 256, max 1024).

---

### Redirect

```
GET /{code}
```

- **Regular visitors:** 302 redirect to the original URL
- **Crawlers** (Facebook, Twitter, LinkedIn, Slack, Telegram, Discord): serves an HTML page with OG meta tags, then auto-redirects via `<meta http-equiv="refresh">`
- **Expired links:** 410 Gone
- **Unknown codes:** 404 Not Found

---

### Health Check

```
GET /health
```

**Response (200):** `{"status":"ok"}`

## Configuration

All settings are configured via environment variables (or a `.env` file):

| Variable               | Default                  | Description                          |
|------------------------|--------------------------|--------------------------------------|
| `PORT`                 | `8080`                   | Server port                          |
| `BASE_URL`             | `http://localhost:8080`  | Public base URL for short links      |
| `ADMIN_SECRET`         | (none)                   | Bearer token for admin endpoints     |
| `DATABASE_URL`         | `postgres://...`         | PostgreSQL connection string         |
| `REDIS_URL`            | `redis://localhost:6379` | Redis connection string              |
| `REDIS_CACHE_TTL`      | `3600`                   | Cache TTL in seconds                 |
| `CLICK_BUFFER_SIZE`    | `1000`                   | Click buffer before flush            |
| `CLICK_FLUSH_INTERVAL` | `5`                      | Click flush interval in seconds      |
| `OG_SCRAPE_TIMEOUT`    | `5`                      | OG scrape timeout in seconds         |
| `OG_SCRAPE_MAX_BODY`   | `1048576`                | Max response body to parse (1MB)     |

## Project Structure

```
url-shorten/
├── cmd/server/main.go           # Entry point, router setup, graceful shutdown
├── internal/
│   ├── config/config.go         # Env-based configuration
│   ├── handler/
│   │   ├── admin.go             # Admin API key management
│   │   ├── link.go              # Link CRUD handlers
│   │   ├── redirect.go          # Redirect + crawler OG serving
│   │   └── qr.go                # QR code generation
│   ├── middleware/
│   │   ├── auth.go              # API key auth + admin auth
│   │   └── crawler.go           # Crawler User-Agent detection
│   ├── model/link.go            # Domain models + DTOs
│   ├── repository/link.go       # PostgreSQL queries
│   ├── cache/redis.go           # Redis cache layer
│   ├── service/link.go          # Business logic + async click counter
│   ├── scraper/og.go            # OG metadata scraper
│   └── shortcode/base62.go      # Base62 encoding
├── migrations/001_init.sql      # Database schema
├── Dockerfile                   # Multi-stage build
├── docker-compose.yml           # App + Postgres + Redis
└── .env.example                 # Config template
```

## Design Decisions

- **Base62 sequence starting at 10000** — produces 3+ character codes from the start, avoids odd-looking 1-2 char codes.
- **302 over 301** — browsers cache 301 redirects permanently, bypassing the server on subsequent visits. 302 ensures every click is tracked.
- **Async click counting** — redirects push to a buffered Go channel. A background goroutine batch-updates PostgreSQL on a timer, keeping redirect latency minimal.
- **Redis cache (1h TTL)** — balances memory with hit rate. Expired links are naturally evicted.
- **API keys hashed with SHA-256** — raw keys are never stored. Only shown once at creation time.
- **OG scrape at creation time** — metadata is fetched once and stored in the DB, avoiding latency on redirect and preventing repeated requests to the original site.
- **Constant-time admin secret comparison** — prevents timing-based attacks on the admin endpoint.
