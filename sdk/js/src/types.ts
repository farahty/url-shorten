// Client configuration

export interface UrlShortenConfig {
  /** Base URL of the URL shortener service (e.g. "https://short.example.com") */
  baseUrl: string;
  /** API key for link operations (sent as X-API-Key header) */
  apiKey?: string;
  /** Admin token for admin operations (sent as Bearer token) */
  adminToken?: string;
}

// Link types

export interface CreateLinkRequest {
  /** The URL to shorten */
  url: string;
  /** Custom alias (1-32 alphanumeric/hyphen/underscore chars) */
  alias?: string;
  /** Expiration time in seconds (max 1 year) */
  expiresIn?: number;
}

export interface OGData {
  title?: string;
  description?: string;
  image?: string;
  siteName?: string;
}

export interface CreateLinkResponse {
  code: string;
  shortUrl: string;
  originalUrl: string;
  expiresAt?: string;
  qrUrl: string;
  og?: OGData;
  createdAt: string;
}

export interface LinkInfoResponse {
  code: string;
  shortUrl: string;
  originalUrl: string;
  clickCount: number;
  isAlias: boolean;
  expiresAt?: string;
  createdAt: string;
}

export interface ListLinksQuery {
  page?: number;
  limit?: number;
}

export interface ListLinksResponse {
  links: LinkInfoResponse[];
  total: number;
  page: number;
  limit: number;
}

// Admin types

export interface CreateApiKeyRequest {
  /** Application name (required) */
  appName: string;
  /** Base URL for the application (optional, must be valid http/https) */
  baseUrl?: string;
}

export interface CreateApiKeyResponse {
  id: string;
  apiKey: string;
  appName: string;
  baseUrl?: string;
  createdAt: string;
}

export interface ApiKeyInfoResponse {
  id: string;
  appName: string;
  baseUrl?: string;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

// Health types

export interface HealthResponse {
  status: string;
  reason?: string;
}

// QR types

export interface QrOptions {
  /** QR code size in pixels (default 256, max 1024) */
  size?: number;
}
