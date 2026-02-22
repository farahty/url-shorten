package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/farahty/url-shorten/internal/repository"
)

type contextKey string

const APIKeyIDKey contextKey = "api_key_id"

func APIKeyAuth(repo *repository.LinkRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, `{"error":"missing API key"}`, http.StatusUnauthorized)
				return
			}

			hash := sha256.Sum256([]byte(apiKey))
			keyHash := hex.EncodeToString(hash[:])

			key, err := repo.GetAPIKeyByHash(r.Context(), keyHash)
			if err != nil {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
				return
			}

			if !key.IsActive {
				http.Error(w, `{"error":"API key is disabled"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), "api_key_id", key.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
