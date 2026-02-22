package model

import "time"

type Link struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	OriginalURL string     `json:"original_url"`
	IsAlias     bool       `json:"is_alias"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ClickCount  int64      `json:"click_count"`
	APIKeyID    string     `json:"api_key_id"`
	OGTitle     *string    `json:"og_title,omitempty"`
	OGDesc      *string    `json:"og_desc,omitempty"`
	OGImage     *string    `json:"og_image,omitempty"`
	OGSite      *string    `json:"og_site,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type APIKey struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"-"`
	AppName   string    `json:"app_name"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateLinkRequest struct {
	URL       string `json:"url"`
	Alias     string `json:"alias,omitempty"`
	ExpiresIn *int64 `json:"expires_in,omitempty"`
}

type CreateLinkResponse struct {
	Code        string     `json:"code"`
	ShortURL    string     `json:"short_url"`
	OriginalURL string     `json:"original_url"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	QRURL       string     `json:"qr_url"`
	OG          *OGData    `json:"og,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type OGData struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
}

type LinkInfoResponse struct {
	Code        string     `json:"code"`
	ShortURL    string     `json:"short_url"`
	OriginalURL string     `json:"original_url"`
	ClickCount  int64      `json:"click_count"`
	IsAlias     bool       `json:"is_alias"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ListLinksResponse struct {
	Links []LinkInfoResponse `json:"links"`
	Total int                `json:"total"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
}

type CreateAPIKeyRequest struct {
	AppName string `json:"app_name"`
}

type CreateAPIKeyResponse struct {
	ID        string    `json:"id"`
	APIKey    string    `json:"api_key"`
	AppName   string    `json:"app_name"`
	CreatedAt time.Time `json:"created_at"`
}

type APIKeyInfoResponse struct {
	ID        string    `json:"id"`
	AppName   string    `json:"app_name"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
