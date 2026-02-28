# @hubflora/url-shorten

TypeScript client SDK for the URL Shortener API. Zero dependencies, ESM-only, uses native `fetch`.

## Install

```bash
npm install @hubflora/url-shorten
```

## Quick Start

```ts
import { UrlShorten } from "@hubflora/url-shorten";

const client = new UrlShorten({
  baseUrl: "https://short.example.com",
  apiKey: "your-api-key",
});

// Create a short link
const link = await client.links.create({ url: "https://example.com" });
console.log(link.shortUrl);

// List links
const { links, total } = await client.links.list({ page: 1, limit: 10 });

// Get link info
const info = await client.links.get("abc123");

// Delete a link
await client.links.delete("abc123");

// Get QR code as Blob
const qr = await client.links.qr("abc123", { size: 512 });
```

## Admin Operations

```ts
const admin = new UrlShorten({
  baseUrl: "https://short.example.com",
  adminToken: "admin-secret",
});

// Create an API key
const key = await admin.admin.createApiKey({ appName: "my-app" });
console.log(key.apiKey);

// List API keys
const keys = await admin.admin.listApiKeys();

// Deactivate an API key
await admin.admin.deactivateApiKey(key.id);
```

## Health Check

```ts
const health = await client.health();
// { status: "ok" }
```

## Error Handling

```ts
import {
  NotFoundError,
  GoneError,
  ConflictError,
  UnauthorizedError,
  UrlShortenError,
} from "@hubflora/url-shorten";

try {
  await client.links.get("missing");
} catch (err) {
  if (err instanceof NotFoundError) {
    // 404 - link not found
  } else if (err instanceof GoneError) {
    // 410 - link expired
  } else if (err instanceof ConflictError) {
    // 409 - alias already taken
  } else if (err instanceof UnauthorizedError) {
    // 401 - invalid API key
  } else if (err instanceof UrlShortenError) {
    // other HTTP error
    console.log(err.status, err.message);
  }
}
```
