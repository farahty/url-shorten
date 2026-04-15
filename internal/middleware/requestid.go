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
