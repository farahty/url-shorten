import {
  ConflictError,
  ForbiddenError,
  GoneError,
  NotFoundError,
  UnauthorizedError,
  UrlShortenError,
} from "./errors.js";
import type {
  ApiKeyInfoResponse,
  CreateApiKeyRequest,
  CreateApiKeyResponse,
  CreateLinkRequest,
  CreateLinkResponse,
  HealthResponse,
  LinkInfoResponse,
  ListLinksQuery,
  ListLinksResponse,
  OGData,
  QrOptions,
  UrlShortenConfig,
} from "./types.js";

function generateRequestId(): string {
  // 16 hex chars, matches the server's safeID regex. Uses crypto.getRandomValues
  // which is available in every supported runtime (Node >= 18, browsers, workers).
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

const REQUEST_ID_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

// snake_case → camelCase mapping helpers

function toCamelCase(str: string): string {
  return str.replace(/_([a-z])/g, (_, c) => c.toUpperCase());
}

function mapKeys(obj: unknown): unknown {
  if (Array.isArray(obj)) return obj.map(mapKeys);
  if (obj !== null && typeof obj === "object") {
    return Object.fromEntries(
      Object.entries(obj as Record<string, unknown>).map(([k, v]) => [
        toCamelCase(k),
        mapKeys(v),
      ]),
    );
  }
  return obj;
}

function toSnakeCase(str: string): string {
  return str.replace(/[A-Z]/g, (c) => `_${c.toLowerCase()}`);
}

function mapKeysToSnake(obj: unknown): unknown {
  if (Array.isArray(obj)) return obj.map(mapKeysToSnake);
  if (obj !== null && typeof obj === "object") {
    return Object.fromEntries(
      Object.entries(obj as Record<string, unknown>).map(([k, v]) => [
        toSnakeCase(k),
        mapKeysToSnake(v),
      ]),
    );
  }
  return obj;
}

// Error factory

function throwForStatus(status: number, message: string): never {
  switch (status) {
    case 401:
      throw new UnauthorizedError(message);
    case 403:
      throw new ForbiddenError(message);
    case 404:
      throw new NotFoundError(message);
    case 409:
      throw new ConflictError(message);
    case 410:
      throw new GoneError(message);
    default:
      throw new UrlShortenError(status, message);
  }
}

export class UrlShorten {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly adminToken?: string;

  readonly links: LinksMethods;
  readonly admin: AdminMethods;

  constructor(config: UrlShortenConfig) {
    this.baseUrl = config.baseUrl.replace(/\/+$/, "");
    this.apiKey = config.apiKey;
    this.adminToken = config.adminToken;

    this.links = new LinksMethods(this);
    this.admin = new AdminMethods(this);
  }

  /** @internal */
  async request<T>(
    path: string,
    options: {
      method?: string;
      body?: unknown;
      auth?: "apiKey" | "admin";
      raw?: boolean;
      query?: Record<string, string | number | undefined>;
      requestId?: string;
    } = {},
  ): Promise<T> {
    const { method = "GET", body, auth, raw = false, query, requestId } = options;

    let url = `${this.baseUrl}${path}`;
    if (query) {
      const params = new URLSearchParams();
      for (const [k, v] of Object.entries(query)) {
        if (v !== undefined) params.set(k, String(v));
      }
      const qs = params.toString();
      if (qs) url += `?${qs}`;
    }

    const headers: Record<string, string> = {};

    if (auth === "apiKey") {
      if (!this.apiKey) throw new Error("apiKey is required for this operation");
      headers["X-API-Key"] = this.apiKey;
    } else if (auth === "admin") {
      if (!this.adminToken)
        throw new Error("adminToken is required for this operation");
      headers["Authorization"] = `Bearer ${this.adminToken}`;
    }

    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    const rid =
      requestId && REQUEST_ID_PATTERN.test(requestId)
        ? requestId
        : generateRequestId();
    headers["X-Request-ID"] = rid;

    const res = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(mapKeysToSnake(body)) : undefined,
    });

    if (!res.ok) {
      let message = res.statusText;
      try {
        const err = (await res.json()) as { error?: string };
        if (err.error) message = err.error;
      } catch {
        // use statusText
      }
      throwForStatus(res.status, message);
    }

    if (res.status === 204) return undefined as T;

    if (raw) return res.blob() as T;

    const json = await res.json();
    return mapKeys(json) as T;
  }

  /** Check service health */
  async health(): Promise<HealthResponse> {
    return this.request<HealthResponse>("/health");
  }
}

class LinksMethods {
  constructor(private readonly client: UrlShorten) {}

  /** Create a shortened link */
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

  /** List all links for the current API key */
  async list(query?: ListLinksQuery): Promise<ListLinksResponse> {
    return this.client.request<ListLinksResponse>("/api/v1/links", {
      auth: "apiKey",
      query: query as Record<string, string | number | undefined>,
    });
  }

  /** Get link info by code */
  async get(code: string): Promise<LinkInfoResponse> {
    return this.client.request<LinkInfoResponse>(`/api/v1/links/${encodeURIComponent(code)}`, {
      auth: "apiKey",
    });
  }

  /** Delete a link by code */
  async delete(code: string): Promise<void> {
    return this.client.request<void>(`/api/v1/links/${encodeURIComponent(code)}`, {
      method: "DELETE",
      auth: "apiKey",
    });
  }

  /** Get QR code image for a link (returns Blob) */
  async qr(code: string, options?: QrOptions): Promise<Blob> {
    return this.client.request<Blob>(`/api/v1/links/${encodeURIComponent(code)}/qr`, {
      auth: "apiKey",
      raw: true,
      query: { size: options?.size },
    });
  }
}

class AdminMethods {
  constructor(private readonly client: UrlShorten) {}

  /** Create a new API key */
  async createApiKey(req: CreateApiKeyRequest): Promise<CreateApiKeyResponse> {
    return this.client.request<CreateApiKeyResponse>("/admin/api-keys", {
      method: "POST",
      body: req,
      auth: "admin",
    });
  }

  /** List all API keys */
  async listApiKeys(): Promise<ApiKeyInfoResponse[]> {
    return this.client.request<ApiKeyInfoResponse[]>("/admin/api-keys", {
      auth: "admin",
    });
  }

  /** Deactivate an API key */
  async deactivateApiKey(id: string): Promise<void> {
    return this.client.request<void>(`/admin/api-keys/${encodeURIComponent(id)}`, {
      method: "DELETE",
      auth: "admin",
    });
  }
}
