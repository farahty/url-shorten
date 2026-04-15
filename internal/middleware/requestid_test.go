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
