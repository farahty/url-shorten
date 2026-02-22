package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/farahty/url-shorten/internal/repository"
)

type contextKey string

const APIKeyIDKey contextKey = "api_key_id"

func APIKeyAuth(repo *repository.LinkRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				writeJSONError(w, "missing API key", http.StatusUnauthorized)
				return
			}

			hash := sha256.Sum256([]byte(apiKey))
			keyHash := hex.EncodeToString(hash[:])

			key, err := repo.GetAPIKeyByHash(r.Context(), keyHash)
			if err != nil {
				writeJSONError(w, "invalid API key", http.StatusUnauthorized)
				return
			}

			if !key.IsActive {
				writeJSONError(w, "API key is disabled", http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), "api_key_id", key.ID)
			if key.BaseURL != nil {
				ctx = context.WithValue(ctx, "app_base_url", *key.BaseURL)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AdminAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				writeJSONError(w, "admin API is not configured", http.StatusServiceUnavailable)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				writeJSONError(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				writeJSONError(w, "invalid authorization format, expected: Bearer <token>", http.StatusUnauthorized)
				return
			}

			if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(secret)) != 1 {
				writeJSONError(w, "invalid admin secret", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
