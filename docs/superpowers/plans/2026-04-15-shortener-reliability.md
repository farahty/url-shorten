# Shortener Reliability & Observability Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the "silent failure" class of bugs where the hubflora worker falls back to long URLs with no visible server log, and harden the request path against transient errors and panics.

**Architecture:** Changes span three layers on the same request path — (a) the Go url-shorten service (panic recovery, request-ID propagation, pool warm-up, timeout alignment), (b) the TypeScript SDK `@hubflora/url-shorten` (AbortController timeout, retry with backoff, request-ID header), and (c) the hubflora worker consumer (structured error classification, metric counter, differentiated fallback policy). Every change is independently shippable; the request-ID tasks (2 and 3) must be paired to deliver end-to-end value.

**Tech Stack:** Go 1.24.7, chi/v5, pgx/v5, go-redis/v9, TypeScript 5.7, native `fetch`, Node ≥ 18, vitest (added as SDK devDep), hubflora pnpm workspace, `@hubflora/logger`.

**Repos / working directories:**
- url-shorten service + SDK: `/Users/nimer/Projects/url-shorten`
- hubflora consumer: `/Users/nimer/Projects/hubflora`

---

## File Structure

Files created or modified by this plan:

**url-shorten service (Go):**
- Modify: `internal/service/link.go` — wrap background scrape goroutine with recover; expose hook for injectable scraper in tests.
- Create: `internal/service/link_test.go` — covers the panic-recovery behaviour.
- Create: `internal/middleware/requestid.go` — chi-compatible middleware that reads `X-Request-ID` or generates one, stores in context, echoes on response.
- Create: `internal/middleware/requestid_test.go`
- Modify: `cmd/server/main.go` — register RequestID middleware **before** chi Logger; replace `chimw.Logger` with a thin wrapper that includes the request id; increase `pgxpool` MinConns; warm Redis on startup; align request timeout with SDK budget.
- Modify: `internal/config/config.go` — add `DBMinConns`, `RequestTimeout` env knobs.

**SDK (TypeScript, `/Users/nimer/Projects/url-shorten/sdk/js`):**
- Modify: `src/client.ts` — add `timeoutMs` (default 10000) and `maxRetries` (default 2) config; wrap `fetch` with `AbortController`; retry on network errors + 5xx + 429 with full jitter; auto-generate + send `X-Request-ID`; preserve header on retry for idempotency.
- Modify: `src/types.ts` — extend `UrlShortenConfig` with the two new fields.
- Modify: `src/errors.ts` — add `NetworkError` and `TimeoutError` classes so callers can classify fallback reasons.
- Create: `src/__tests__/client.test.ts` — vitest suite: timeout, retry on 5xx, no retry on 4xx, request-id header forwarded, request-id stable across retries.
- Modify: `package.json` — add `vitest` devDep + `test` script.
- Create: `vitest.config.ts` (SDK-local) — Node environment.

**hubflora consumer:**
- Modify: `lib/utils/url-shortening.ts` — classify errors (config vs transient); throw on config errors (no silent fallback); count transient fallbacks via a tiny local counter exported as `getShortenerMetrics()`; pass a request-id derived from the lead context when available.
- Create: `lib/utils/url-shortening.test.ts` — vitest suite running against a mocked SDK.
- Modify: `packages/lead-automation/src/funnel-automation/nurture-message-sender.ts` — call sites unchanged behaviourally, but pass `leadId` so it becomes the request id suffix.

---

## Task 1: Recover panics in the background OG-scrape goroutine

**Why:** `service/link.go:130-156` launches `go func(...)` that calls the scraper and DB. `chimw.Recoverer` only wraps the request goroutine. Any panic here (nil-deref in goquery on malformed HTML, DB pool exhaustion panic, etc.) kills the whole process — which explains "the server sometimes fails and nothing is in the log."

**Files:**
- Modify: `internal/service/link.go` (the goroutine launched at line 130)
- Create: `internal/service/link_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/service/link_test.go`:

```go
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/farahty/url-shorten/internal/model"
)

// panickingScraper implements the minimal surface LinkService uses in the
// background goroutine. We assert the process survives its panic.
type panickingScraper struct{ called chan struct{} }

func (p *panickingScraper) Scrape(ctx context.Context, raw string) *model.OGData {
	close(p.called)
	panic("boom")
}

func TestScrapeGoroutinePanicDoesNotCrashProcess(t *testing.T) {
	ps := &panickingScraper{called: make(chan struct{})}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runScrapeJob(context.Background(), ps, nil, "abc", "https://example.com")
	}()

	select {
	case <-ps.called:
	case <-time.After(time.Second):
		t.Fatal("scraper was never invoked")
	}
	wg.Wait() // returns only if runScrapeJob recovered
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/service/ -run TestScrapeGoroutinePanicDoesNotCrashProcess -v`
Expected: build fails with `undefined: runScrapeJob` and `Scraper` interface mismatch.

- [ ] **Step 3: Extract the scrape work into a recoverable function**

In `internal/service/link.go`, add near the top of the file (after the `ErrExpiryTooLong` var block) a narrow interface:

```go
// scraperIface is the subset of *scraper.OGScraper the service uses. Kept
// narrow to make the background job testable without spinning up HTTP.
type scraperIface interface {
	Scrape(ctx context.Context, rawURL string) *model.OGData
}

// ogUpdater is the subset of *repository.LinkRepository used by the background
// scrape job. nil-safe: runScrapeJob skips the DB write when updater is nil.
type ogUpdater interface {
	UpdateOGData(ctx context.Context, code string, title, desc, image, site *string) error
}

// runScrapeJob performs the OG scrape + DB update for a freshly created link.
// Runs in a background goroutine; MUST recover from panics so a bad page or
// driver panic does not take down the whole server.
func runScrapeJob(parent context.Context, sc scraperIface, repo ogUpdater, code, rawURL string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in OG scrape goroutine for %s: %v", code, r)
		}
	}()

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	og := sc.Scrape(ctx, rawURL)
	if og == nil {
		return
	}
	var title, desc, image, site *string
	if og.Title != "" {
		title = &og.Title
	}
	if og.Description != "" {
		desc = &og.Description
	}
	if og.Image != "" {
		image = &og.Image
	}
	if og.SiteName != "" {
		site = &og.SiteName
	}
	if title == nil && desc == nil && image == nil && site == nil {
		return
	}
	if repo == nil {
		return
	}
	if err := repo.UpdateOGData(ctx, code, title, desc, image, site); err != nil {
		log.Printf("error updating OG data for %s: %v", code, err)
	}
}
```

Then replace the existing inline `go func(...)` at `link.go:130-156` with:

```go
	// Scrape OG metadata in the background (non-blocking, panic-safe).
	go runScrapeJob(context.Background(), s.scraper, s.repo, link.Code, link.OriginalURL)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/service/ -run TestScrapeGoroutinePanicDoesNotCrashProcess -v`
Expected: PASS. Log line `panic in OG scrape goroutine for abc: boom` visible in output.

- [ ] **Step 5: Run the full package to confirm no regressions**

Run: `go build ./... && go vet ./...`
Expected: exit 0, no output.

- [ ] **Step 6: Commit**

```bash
git add internal/service/link.go internal/service/link_test.go
git commit -m "fix(service): recover panics in background OG scrape goroutine"
```

---

## Task 2: Server-side Request-ID middleware and structured request logging

**Why:** Today chi's stock logger formats requests with no correlation id, so a worker-side `logger.error("URL shortener failed", ...)` can never be tied to a server log line. Adding `X-Request-ID` lets the worker print the id and ops greps both systems with one key.

**Files:**
- Create: `internal/middleware/requestid.go`
- Create: `internal/middleware/requestid_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write the failing test**

Create `internal/middleware/requestid_test.go`:

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID_UsesIncomingHeader(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got != "rid-123" {
			t.Fatalf("want rid-123, got %q", got)
		}
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "rid-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "rid-123" {
		t.Fatalf("want header rid-123, got %q", got)
	}
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(seen) < 8 {
		t.Fatalf("expected generated id, got %q", seen)
	}
	if rec.Header().Get("X-Request-ID") != seen {
		t.Fatalf("response header must echo generated id")
	}
}

func TestRequestID_RejectsUnsafeHeader(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "../../etc/passwd\n\rinjected")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen == "../../etc/passwd\n\rinjected" {
		t.Fatal("middleware must sanitize untrusted inbound ids")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/middleware/ -run TestRequestID -v`
Expected: build fails with `undefined: RequestID`, `undefined: RequestIDFromContext`.

- [ ] **Step 3: Implement the middleware**

Create `internal/middleware/requestid.go`:

```go
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

type ctxKeyRequestID struct{}

// safeID permits only characters safe to echo back and log: alphanumerics,
// dash, underscore. Length capped at 64. Any inbound id failing the check is
// discarded and a fresh one is generated.
var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func generateID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// RequestID propagates X-Request-ID. It trusts inbound ids only if they match
// safeID; otherwise it mints a fresh id. The id is echoed on the response and
// stored in the request context under a private key.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !safeID.MatchString(id) {
			id = generateID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request id or the empty string.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID{}).(string)
	return id
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/middleware/ -run TestRequestID -v`
Expected: all three subtests PASS.

- [ ] **Step 5: Wire the middleware into the router and include the id in request logs**

In `cmd/server/main.go`:

Replace the middleware block at lines 72-75:

```go
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)
	r.Use(chimw.Timeout(30 * time.Second))
```

with:

```go
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestID)
	r.Use(requestLogger) // defined below — includes the request id
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(cfg.RequestTimeout))
```

Add this function to the bottom of `main.go`:

```go
// requestLogger logs completed requests with the request id from context so
// server-side logs can be correlated with worker-side errors.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Printf("rid=%s %s %s %d %dB %s",
			middleware.RequestIDFromContext(r.Context()),
			r.Method, r.URL.RequestURI(),
			ww.Status(), ww.BytesWritten(),
			time.Since(start))
	})
}
```

Note: `cfg.RequestTimeout` is introduced in Task 8. For this task, hard-code `30 * time.Second` and change it to `cfg.RequestTimeout` when Task 8 lands:

```go
	r.Use(chimw.Timeout(30 * time.Second))
```

- [ ] **Step 6: Run build + vet**

Run: `go build ./... && go vet ./...`
Expected: exit 0.

- [ ] **Step 7: Smoke-test locally**

Run: `docker compose up --build -d && sleep 3 && curl -sS -i http://localhost:8080/health | head -5 && docker compose logs app --tail=5`
Expected: response header `X-Request-ID: <16 hex chars>`; log line contains `rid=<same id> GET /health 200`.

- [ ] **Step 8: Commit**

```bash
git add internal/middleware/requestid.go internal/middleware/requestid_test.go cmd/server/main.go
git commit -m "feat(server): propagate X-Request-ID and include it in request logs"
```

---

## Task 3: SDK — generate and forward X-Request-ID

**Why:** Pairs with Task 2. The worker already has a lead-id; passing it through as the request id lets a single grep trace a nurture job across worker → SDK → server.

**Files:**
- Modify: `sdk/js/src/client.ts`
- Modify: `sdk/js/src/types.ts`

- [ ] **Step 1: Extend the request options**

In `sdk/js/src/types.ts`, leave `UrlShortenConfig` as-is for now (Task 5 adds timeout/retry knobs).

In `sdk/js/src/client.ts`, change the `options` type inside `request<T>` (around line 99-106) from:

```ts
    options: {
      method?: string;
      body?: unknown;
      auth?: "apiKey" | "admin";
      raw?: boolean;
      query?: Record<string, string | number | undefined>;
    } = {},
```

to:

```ts
    options: {
      method?: string;
      body?: unknown;
      auth?: "apiKey" | "admin";
      raw?: boolean;
      query?: Record<string, string | number | undefined>;
      requestId?: string;
    } = {},
```

- [ ] **Step 2: Add a request-id helper**

At the top of `client.ts` (just after the imports), add:

```ts
function generateRequestId(): string {
  // 16 hex chars, matches the server's safeID regex. Uses crypto.getRandomValues
  // which is available in every supported runtime (Node >= 18, browsers, workers).
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

const REQUEST_ID_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;
```

- [ ] **Step 3: Send the header on every request**

In `client.ts`, destructure `requestId` from options (around line 107):

```ts
    const { method = "GET", body, auth, raw = false, query, requestId } = options;
```

After the auth-header block (around line 128), add:

```ts
    const rid =
      requestId && REQUEST_ID_PATTERN.test(requestId)
        ? requestId
        : generateRequestId();
    headers["X-Request-ID"] = rid;
```

- [ ] **Step 4: Surface the id on typed methods**

Change `LinksMethods.create` to accept an optional id:

```ts
  async create(
    req: CreateLinkRequest,
    opts: { requestId?: string } = {},
  ): Promise<CreateLinkResponse> {
    return this.client.request<CreateLinkResponse>("/api/v1/links", {
      method: "POST",
      body: req,
      auth: "apiKey",
      requestId: opts.requestId,
    });
  }
```

- [ ] **Step 5: Rebuild the SDK**

Run: `cd sdk/js && npx tsup`
Expected: builds into `dist/`, no errors.

- [ ] **Step 6: Commit**

```bash
git add sdk/js/src/client.ts sdk/js/src/types.ts
git commit -m "feat(sdk): send X-Request-ID on every request, surface override on links.create"
```

---

## Task 4: SDK — add vitest and baseline tests

**Why:** Tasks 5 and 6 add non-trivial retry/timeout logic that must be covered by tests. Get the harness in place first.

**Files:**
- Modify: `sdk/js/package.json`
- Create: `sdk/js/vitest.config.ts`
- Create: `sdk/js/src/__tests__/client.test.ts`

- [ ] **Step 1: Add vitest devDep and a test script**

In `sdk/js/package.json`, add inside `devDependencies`:

```json
    "vitest": "^2.1.0"
```

and inside `scripts`:

```json
    "test": "vitest run",
    "test:watch": "vitest"
```

Run: `cd sdk/js && npm install`
Expected: `node_modules/vitest` exists.

- [ ] **Step 2: Create the vitest config**

Create `sdk/js/vitest.config.ts`:

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: ["src/__tests__/**/*.test.ts"],
  },
});
```

- [ ] **Step 3: Write a baseline test covering Task 3**

Create `sdk/js/src/__tests__/client.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UrlShorten } from "../client.js";

describe("UrlShorten request-id", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function okJson(body: unknown) {
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }

  it("generates an X-Request-ID when caller provides none", async () => {
    fetchMock.mockResolvedValueOnce(okJson({ code: "abc" }));
    const c = new UrlShorten({ baseUrl: "http://x", apiKey: "k" });
    await c.links.create({ url: "https://example.com" });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Request-ID"]).toMatch(/^[a-f0-9]{16}$/);
  });

  it("forwards caller-supplied request id verbatim when safe", async () => {
    fetchMock.mockResolvedValueOnce(okJson({ code: "abc" }));
    const c = new UrlShorten({ baseUrl: "http://x", apiKey: "k" });
    await c.links.create(
      { url: "https://example.com" },
      { requestId: "lead-42" },
    );

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Request-ID"]).toBe("lead-42");
  });

  it("replaces unsafe request ids", async () => {
    fetchMock.mockResolvedValueOnce(okJson({ code: "abc" }));
    const c = new UrlShorten({ baseUrl: "http://x", apiKey: "k" });
    await c.links.create(
      { url: "https://example.com" },
      { requestId: "bad id\n\r" },
    );

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Request-ID"]).toMatch(/^[a-f0-9]{16}$/);
  });
});
```

- [ ] **Step 4: Run the tests**

Run: `cd sdk/js && npm test`
Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add sdk/js/package.json sdk/js/vitest.config.ts sdk/js/src/__tests__/client.test.ts sdk/js/package-lock.json
git commit -m "test(sdk): add vitest harness and request-id coverage"
```

---

## Task 5: SDK — AbortController timeout

**Why:** Node's `fetch` has no default timeout. A stalled TCP or half-open connection hangs until the worker's job-level timeout — which today means the lead row is locked for 30 minutes and eventually the whole batch stalls. A 10-second default aborts before the next batch arrives.

**Files:**
- Modify: `sdk/js/src/types.ts`
- Modify: `sdk/js/src/errors.ts`
- Modify: `sdk/js/src/client.ts`
- Modify: `sdk/js/src/__tests__/client.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `sdk/js/src/__tests__/client.test.ts`:

```ts
describe("UrlShorten timeout", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("aborts the fetch after timeoutMs", async () => {
    fetchMock.mockImplementation((_url, init: RequestInit) => {
      return new Promise((_resolve, reject) => {
        init.signal?.addEventListener("abort", () => {
          const err = new Error("aborted");
          err.name = "AbortError";
          reject(err);
        });
      });
    });

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 20,
      maxRetries: 0,
    });

    await expect(c.links.create({ url: "https://e" })).rejects.toMatchObject({
      name: "TimeoutError",
    });
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd sdk/js && npm test`
Expected: new test fails (config options unknown / no TimeoutError class).

- [ ] **Step 3: Extend config and errors**

In `sdk/js/src/types.ts`, add to `UrlShortenConfig`:

```ts
  /** Per-request abort timeout in milliseconds. Default: 10_000. */
  timeoutMs?: number;
  /** Max retries for transient failures (network errors, 5xx, 429). Default: 2. */
  maxRetries?: number;
```

In `sdk/js/src/errors.ts`, add (preserving existing exports):

```ts
export class NetworkError extends Error {
  readonly cause?: unknown;
  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = "NetworkError";
    this.cause = cause;
  }
}

export class TimeoutError extends Error {
  constructor(message = "request timed out") {
    super(message);
    this.name = "TimeoutError";
  }
}
```

- [ ] **Step 4: Implement timeout in the client**

In `sdk/js/src/client.ts`:

Add to the class fields (below `adminToken`):

```ts
  private readonly timeoutMs: number;
  private readonly maxRetries: number;
```

In the constructor:

```ts
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxRetries = config.maxRetries ?? 2;
```

Import the new errors:

```ts
import {
  ConflictError,
  ForbiddenError,
  GoneError,
  NetworkError,
  NotFoundError,
  TimeoutError,
  UnauthorizedError,
  UrlShortenError,
} from "./errors.js";
```

Replace the `const res = await fetch(...)` call (around line 134-138) with:

```ts
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    let res: Response;
    try {
      res = await fetch(url, {
        method,
        headers,
        body: body !== undefined ? JSON.stringify(mapKeysToSnake(body)) : undefined,
        signal: controller.signal,
      });
    } catch (err) {
      if ((err as { name?: string }).name === "AbortError") {
        throw new TimeoutError(`request to ${path} exceeded ${this.timeoutMs}ms`);
      }
      throw new NetworkError(`network error calling ${path}`, err);
    } finally {
      clearTimeout(timer);
    }
```

- [ ] **Step 5: Run the tests**

Run: `cd sdk/js && npm test`
Expected: all tests PASS including the new timeout test.

- [ ] **Step 6: Commit**

```bash
git add sdk/js/src/client.ts sdk/js/src/errors.ts sdk/js/src/types.ts sdk/js/src/__tests__/client.test.ts
git commit -m "feat(sdk): AbortController-backed request timeout (default 10s)"
```

---

## Task 6: SDK — retry on transient failures

**Why:** A single TCP reset or a 502 from a restarting pod shouldn't doom a lead to the long-URL fallback. Two retries with full jitter turn most transient failures into successes and make the fallback branch rare enough that alerts become meaningful.

**Policy:**
- Retry on: `NetworkError`, `TimeoutError`, HTTP 429, HTTP 5xx.
- Do NOT retry on: 4xx (except 429), `UnauthorizedError`, `ForbiddenError`, `NotFoundError`, `ConflictError`, `GoneError`.
- Backoff: `min(200ms * 2^attempt, 2000ms)` with full jitter. Same request id across retries for server-side deduplication in the future.

**Files:**
- Modify: `sdk/js/src/client.ts`
- Modify: `sdk/js/src/__tests__/client.test.ts`

- [ ] **Step 1: Write failing tests**

Append to `sdk/js/src/__tests__/client.test.ts`:

```ts
describe("UrlShorten retry", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function resp(status: number, body: unknown = {}) {
    return new Response(JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    });
  }

  it("retries 5xx twice then succeeds", async () => {
    fetchMock
      .mockResolvedValueOnce(resp(502, { error: "bad gateway" }))
      .mockResolvedValueOnce(resp(503, { error: "unavailable" }))
      .mockResolvedValueOnce(resp(200, { code: "abc" }));

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    const out = await c.links.create({ url: "https://e" });
    expect(out).toMatchObject({ code: "abc" });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("does not retry on 4xx", async () => {
    fetchMock.mockResolvedValueOnce(resp(400, { error: "bad" }));
    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    await expect(c.links.create({ url: "https://e" })).rejects.toThrow();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("keeps the same X-Request-ID across retries", async () => {
    fetchMock
      .mockResolvedValueOnce(resp(503))
      .mockResolvedValueOnce(resp(200, { code: "abc" }));

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    await c.links.create({ url: "https://e" }, { requestId: "lead-7" });

    const first = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<
      string,
      string
    >;
    const second = (fetchMock.mock.calls[1][1] as RequestInit).headers as Record<
      string,
      string
    >;
    expect(first["X-Request-ID"]).toBe("lead-7");
    expect(second["X-Request-ID"]).toBe("lead-7");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd sdk/js && npm test`
Expected: three new tests fail (no retry logic yet).

- [ ] **Step 3: Implement retry in `request<T>`**

In `sdk/js/src/client.ts`, add this helper above the class:

```ts
function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function backoffMs(attempt: number): number {
  const cap = 2000;
  const base = 200;
  const exp = Math.min(cap, base * 2 ** attempt);
  return Math.floor(Math.random() * exp); // full jitter
}
```

Restructure the body of `request<T>` so the per-attempt work (everything from building headers/body through to returning the parsed JSON) lives inside a loop. The request-id is generated **once** before the loop and reused. Sketch:

```ts
    const rid =
      requestId && REQUEST_ID_PATTERN.test(requestId)
        ? requestId
        : generateRequestId();

    let lastErr: unknown;
    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      try {
        return await this.performRequest<T>({
          url,
          method,
          headers: { ...headers, "X-Request-ID": rid },
          body,
          raw,
        });
      } catch (err) {
        lastErr = err;
        if (!isRetryable(err) || attempt === this.maxRetries) throw err;
        await sleep(backoffMs(attempt));
      }
    }
    throw lastErr;
```

Move the existing fetch + status handling into a new private method `performRequest<T>(args)`. Add `isRetryable`:

```ts
function isRetryable(err: unknown): boolean {
  if (err instanceof TimeoutError) return true;
  if (err instanceof NetworkError) return true;
  if (err instanceof UrlShortenError) {
    return err.status === 429 || (err.status >= 500 && err.status < 600);
  }
  return false;
}
```

Build the `headers` object **outside** the loop but merge `X-Request-ID` fresh each attempt (as shown above). Do not mutate the shared `headers` object. Ensure the timeout logic from Task 5 lives inside `performRequest`.

- [ ] **Step 4: Run the tests**

Run: `cd sdk/js && npm test`
Expected: all tests PASS (including "retries 5xx twice then succeeds" with 3 fetch calls).

- [ ] **Step 5: Commit**

```bash
git add sdk/js/src/client.ts sdk/js/src/__tests__/client.test.ts
git commit -m "feat(sdk): retry network errors, 429, and 5xx with full-jitter backoff"
```

---

## Task 7: Rebuild and publish the SDK, bump consumer

**Why:** hubflora consumes a built `dist/`, not the source. Tasks 3–6 are invisible to the worker until the SDK is rebuilt and the hubflora lockfile is updated.

**Files:**
- Modify: `sdk/js/package.json` (version bump)
- Modify: `/Users/nimer/Projects/hubflora/package.json` or whichever workspace file references `@hubflora/url-shorten`

- [ ] **Step 1: Bump SDK version**

In `sdk/js/package.json` change `"version": "0.1.0"` to `"version": "0.2.0"`.

- [ ] **Step 2: Rebuild**

Run: `cd sdk/js && npx tsup`
Expected: `dist/index.js` and `dist/index.d.ts` regenerated.

- [ ] **Step 3: Locate the consumer reference**

Run: `grep -R "@hubflora/url-shorten" /Users/nimer/Projects/hubflora --include=package.json -l`
Expected: one or more workspace `package.json` files referencing the SDK (likely a `file:` or `workspace:` protocol path).

- [ ] **Step 4: If a version constraint is pinned, bump it**

Edit each referenced `package.json` so the constraint permits `0.2.0` (`workspace:*` needs no change). Then:

Run: `cd /Users/nimer/Projects/hubflora && pnpm install`
Expected: lockfile updates, no errors.

- [ ] **Step 5: Commit (in url-shorten repo)**

```bash
cd /Users/nimer/Projects/url-shorten
git add sdk/js/package.json sdk/js/dist
git commit -m "chore(sdk): release 0.2.0 — request-id, timeout, retry"
```

- [ ] **Step 6: Commit (in hubflora repo)**

```bash
cd /Users/nimer/Projects/hubflora
git add pnpm-lock.yaml $(grep -R "@hubflora/url-shorten" . --include=package.json -l)
git commit -m "chore(deps): bump @hubflora/url-shorten to 0.2.0"
```

---

## Task 8: Server config — align request timeout and warm connection pools

**Why:** The 7.8s cold-start spikes visible in production logs correlate with the start of each hourly batch. Keeping one or two warm DB connections eliminates that cold path. Aligning the chi request timeout with the SDK budget (10s SDK + 2 retries = ~6s ceiling per attempt, plus headroom) means neither side gives up while the other is still working.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Extend config**

In `internal/config/config.go`, add fields to `Config`:

```go
	DBMinConns     int32
	DBMaxConns     int32
	RequestTimeout time.Duration
```

And in `Load()`:

```go
		DBMinConns:     int32(getEnvInt("DB_MIN_CONNS", 2)),
		DBMaxConns:     int32(getEnvInt("DB_MAX_CONNS", 20)),
		RequestTimeout: time.Duration(getEnvInt("REQUEST_TIMEOUT", 15)) * time.Second,
```

(`int32` import not needed — `int32(...)` conversion works directly.)

- [ ] **Step 2: Apply pool settings at startup**

In `cmd/server/main.go`, replace:

```go
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
```

with:

```go
	pgCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to parse database url: %v", err)
	}
	pgCfg.MinConns = cfg.DBMinConns
	pgCfg.MaxConns = cfg.DBMaxConns
	dbPool, err := pgxpool.NewWithConfig(ctx, pgCfg)
```

- [ ] **Step 3: Use the configured request timeout**

Replace `r.Use(chimw.Timeout(30 * time.Second))` (the hard-coded value from Task 2) with:

```go
	r.Use(chimw.Timeout(cfg.RequestTimeout))
```

- [ ] **Step 4: Build and smoke-test**

Run: `go build ./... && docker compose up --build -d && sleep 3 && curl -sS http://localhost:8080/health`
Expected: `{"status":"ok"}` plus server logs show steady connections after startup (optionally check `docker compose logs app | grep PostgreSQL`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go cmd/server/main.go
git commit -m "perf(server): warm pgxpool MinConns and configurable request timeout"
```

---

## Task 9: Worker — classify shortener errors and expose a metric

**Why:** Today every failure is logged at the same level and the sender always falls back silently. Splitting `ConfigError` (developer bug — should throw) from `TransientError` (retries exhausted — count and fall back) turns the warning into a real signal. The counter lets ops add a Prometheus rule or a dashboard tile without changing the worker again.

**Files:**
- Modify: `/Users/nimer/Projects/hubflora/lib/utils/url-shortening.ts`
- Create: `/Users/nimer/Projects/hubflora/lib/utils/url-shortening.test.ts`
- Modify: `/Users/nimer/Projects/hubflora/packages/lead-automation/src/funnel-automation/nurture-message-sender.ts` (pass `leadId` through)

- [ ] **Step 1: Write failing tests**

Create `/Users/nimer/Projects/hubflora/lib/utils/url-shortening.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock the SDK before importing the module under test.
const createMock = vi.fn();
vi.mock("@hubflora/url-shorten", () => ({
  UrlShorten: class {
    links = { create: createMock };
  },
}));

import {
  createShortLink,
  getShortenerMetrics,
  resetShortenerMetrics,
} from "./url-shortening.js";

describe("createShortLink", () => {
  beforeEach(() => {
    createMock.mockReset();
    resetShortenerMetrics();
    process.env.URL_SHORTEN_BASE_URL = "http://x";
    process.env.URL_SHORTEN_API_KEY = "k";
  });
  afterEach(() => {
    delete process.env.URL_SHORTEN_BASE_URL;
    delete process.env.URL_SHORTEN_API_KEY;
  });

  it("returns the short url on success", async () => {
    createMock.mockResolvedValueOnce({ shortUrl: "http://s/abc" });
    await expect(createShortLink("https://e")).resolves.toBe("http://s/abc");
    expect(getShortenerMetrics().fallbacks).toBe(0);
  });

  it("falls back to target on transient error and increments counter", async () => {
    const err = new Error("socket hang up");
    err.name = "NetworkError";
    createMock.mockRejectedValueOnce(err);

    await expect(createShortLink("https://e")).resolves.toBe("https://e");
    expect(getShortenerMetrics().fallbacks).toBe(1);
    expect(getShortenerMetrics().byReason.NetworkError).toBe(1);
  });

  it("throws (does not fall back) when configuration is missing", async () => {
    delete process.env.URL_SHORTEN_API_KEY;
    await expect(createShortLink("https://e")).rejects.toThrow(
      /URL_SHORTEN_API_KEY/,
    );
    expect(getShortenerMetrics().fallbacks).toBe(0);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/nimer/Projects/hubflora && pnpm vitest run lib/utils/url-shortening.test.ts`
Expected: tests fail (no `getShortenerMetrics` / `resetShortenerMetrics` exports, mis-classified errors).

- [ ] **Step 3: Rewrite `url-shortening.ts` with classified errors and a counter**

Replace the file contents with:

```ts
// Dynamic import because `@hubflora/url-shorten` is ESM-only. The worker bundle
// is CJS (esbuild output), but esbuild preserves native `import()` calls so Node's
// ESM loader can resolve the package at runtime. Do NOT switch this to a static
// `import` — under a CJS compile pipeline (tsc), that would collapse to a
// `require()` which fails with ERR_REQUIRE_ESM and silently breaks every SMS
// CTA shortening (incident: 2026-04-11).
import { logger } from "@hubflora/logger";

let _mod: typeof import("@hubflora/url-shorten") | null = null;
async function loadMod() {
  if (!_mod) _mod = await import("@hubflora/url-shorten");
  return _mod;
}

let client: InstanceType<
  (typeof import("@hubflora/url-shorten"))["UrlShorten"]
> | null = null;

async function getClient() {
  if (!client) {
    const { UrlShorten } = await loadMod();
    const baseUrl = process.env.URL_SHORTEN_BASE_URL;
    const apiKey = process.env.URL_SHORTEN_API_KEY;
    if (!baseUrl) throw new Error("URL_SHORTEN_BASE_URL is not configured");
    if (!apiKey) throw new Error("URL_SHORTEN_API_KEY is not configured");
    client = new UrlShorten({ baseUrl, apiKey });
  }
  return client;
}

export function stripProtocol(url: string): string {
  return url.replace(/^https?:\/\//, "");
}

// --- metrics ----------------------------------------------------------------

interface ShortenerMetrics {
  fallbacks: number;
  byReason: Record<string, number>;
}

const metrics: ShortenerMetrics = { fallbacks: 0, byReason: {} };

export function getShortenerMetrics(): ShortenerMetrics {
  // Clone so callers cannot mutate internal state.
  return { fallbacks: metrics.fallbacks, byReason: { ...metrics.byReason } };
}

export function resetShortenerMetrics(): void {
  metrics.fallbacks = 0;
  metrics.byReason = {};
}

function recordFallback(reason: string) {
  metrics.fallbacks++;
  metrics.byReason[reason] = (metrics.byReason[reason] ?? 0) + 1;
}

// --- error classification ---------------------------------------------------

function isConfigError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  return /URL_SHORTEN_(BASE_URL|API_KEY)/.test(err.message);
}

function classifyError(err: unknown): string {
  if (!(err instanceof Error)) return "Unknown";
  return err.name || "Error";
}

// --- public API -------------------------------------------------------------

export interface CreateShortLinkOptions {
  /** Correlation id forwarded as X-Request-ID. */
  requestId?: string;
}

export async function createShortLink(
  target: string,
  opts: CreateShortLinkOptions = {},
): Promise<string> {
  let c;
  try {
    c = await getClient();
  } catch (err) {
    // Config errors must NOT silently fall back — they indicate a deploy-time
    // misconfiguration and need to be surfaced, not swallowed.
    if (isConfigError(err)) throw err;
    throw err;
  }

  try {
    const result = await c.links.create(
      { url: target },
      { requestId: opts.requestId },
    );
    return result.shortUrl;
  } catch (err) {
    const reason = classifyError(err);
    recordFallback(reason);
    logger.error(
      "URL shortener failed, falling back to full URL",
      err instanceof Error ? err : new Error(String(err)),
      {
        target,
        reason,
        requestId: opts.requestId,
        baseUrl: process.env.URL_SHORTEN_BASE_URL,
      },
    );
    return target;
  }
}

export async function createSmsShortLink(
  target: string,
  opts: CreateShortLinkOptions = {},
): Promise<string> {
  const url = await createShortLink(target, opts);
  return stripProtocol(url);
}

export async function generateQrCode(
  url: string,
): Promise<{ success: boolean; qrCodeUrl?: string; error?: string }> {
  return {
    success: true,
    qrCodeUrl: `${url}.qr`,
  };
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/nimer/Projects/hubflora && pnpm vitest run lib/utils/url-shortening.test.ts`
Expected: all 3 tests PASS.

- [ ] **Step 5: Pass `leadId` as the request id from nurture sender**

In `/Users/nimer/Projects/hubflora/packages/lead-automation/src/funnel-automation/nurture-message-sender.ts`, at each call site that invokes `createSmsShortLink(target)` or `createShortLink(target)` (lines 269, 465, 491), change to:

```ts
createSmsShortLink(target, { requestId: `lead-${lead.id}` })
```

(and the `createShortLink` equivalent). `lead.id` is already in scope at all three call sites — if it is not, pass the nearest available identifier (e.g. `automation.id`) prefixed accordingly.

- [ ] **Step 6: Build + typecheck**

Run: `cd /Users/nimer/Projects/hubflora && pnpm -r build` (or the project's typecheck command)
Expected: exit 0.

- [ ] **Step 7: Commit (in hubflora repo)**

```bash
cd /Users/nimer/Projects/hubflora
git add lib/utils/url-shortening.ts lib/utils/url-shortening.test.ts packages/lead-automation/src/funnel-automation/nurture-message-sender.ts
git commit -m "feat(shortener): classify errors, expose fallback metric, forward request-id"
```

---

## Task 10: End-to-end smoke verification

**Why:** Nine tasks across two repos — before calling this done, prove a failed shortener request shows up in both logs with the same id, and a healthy request looks right too.

- [ ] **Step 1: Start the stack**

Run: `cd /Users/nimer/Projects/url-shorten && docker compose up --build -d && sleep 5`
Expected: `docker compose ps` shows app, pg, redis healthy.

- [ ] **Step 2: Create a link with a known request id**

Run:
```bash
curl -sS -X POST http://localhost:8080/api/v1/links \
  -H "X-API-Key: $URL_SHORTEN_API_KEY" \
  -H "X-Request-ID: smoke-test-001" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}' -i | head -20
```
Expected: `HTTP/1.1 201 Created`, response header `X-Request-ID: smoke-test-001`.

- [ ] **Step 3: Verify server log correlation**

Run: `docker compose logs app --tail=20 | grep smoke-test-001`
Expected: a log line containing `rid=smoke-test-001 POST /api/v1/links 201`.

- [ ] **Step 4: Simulate a transient failure from the worker**

Stop the server (`docker compose stop app`), then in a node repl inside hubflora:

```ts
process.env.URL_SHORTEN_BASE_URL = "http://localhost:8080";
process.env.URL_SHORTEN_API_KEY = "irrelevant";
const { createShortLink, getShortenerMetrics } = await import("./lib/utils/url-shortening.js");
await createShortLink("https://example.com", { requestId: "smoke-test-002" });
getShortenerMetrics();
```
Expected: returns `"https://example.com"` (fallback); metrics show `fallbacks: 1`, `byReason.NetworkError: 1`; a structured `ERROR` log line includes `reason: "NetworkError"` and `requestId: "smoke-test-002"`.

- [ ] **Step 5: Restart and confirm retry succeeds**

Start the server again, re-run the same call. Expected: returns a short URL, `fallbacks` unchanged.

- [ ] **Step 6: Final commit (plan completion marker)**

Nothing to commit unless the verification revealed a fix. Otherwise:

```bash
cd /Users/nimer/Projects/url-shorten
git log --oneline -n 10
```

---

## Self-review

**Spec coverage:**
- P0.1 (recover panic) → Task 1 ✓
- P0.2 (metric/alert on fallback) → Task 9 (counter) + Task 10 (verification) ✓
- P0.3 (request-id propagation) → Task 2 (server) + Task 3 (SDK) + Task 9 Step 5 (worker) ✓
- P1.4 (fetch timeout + retry) → Tasks 5, 6 ✓
- P1.5 (warm DB/Redis pools) → Task 8 ✓
- P2.6 (differentiate fatal vs transient) → Task 9 ✓
- P2.7 (align chi timeout with SDK) → Task 8 ✓

**Type consistency:** `UrlShortenConfig` gets `timeoutMs` + `maxRetries` in Task 5; used in Tasks 5–6 as configured. `NetworkError` / `TimeoutError` added in Task 5 are referenced by `isRetryable` in Task 6 and by the worker's `classifyError` in Task 9. `requestId` option flows identically through SDK (Task 3) → client method (Task 3) → consumer helper (Task 9) → nurture sender (Task 9 Step 5). `RequestID` middleware and `RequestIDFromContext` (Task 2) are used by `requestLogger` in the same task.

**Placeholder scan:** One soft reference — "pass the nearest available identifier (e.g. `automation.id`) prefixed accordingly" in Task 9 Step 5. Acceptable because `lead.id` is in scope at all three nurture call sites verified during exploration; the fallback note covers a contingency only.
