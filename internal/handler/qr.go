package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/farahty/url-shorten/internal/config"
	"github.com/farahty/url-shorten/internal/repository"
	"github.com/farahty/url-shorten/internal/service"
	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"
)

type QRHandler struct {
	svc *service.LinkService
	cfg *config.Config
}

func NewQRHandler(svc *service.LinkService, cfg *config.Config) *QRHandler {
	return &QRHandler{svc: svc, cfg: cfg}
}

func (h *QRHandler) GetQRPublic(w http.ResponseWriter, r *http.Request) {
	codeWithExt := chi.URLParam(r, "code")
	code := strings.TrimSuffix(codeWithExt, ".qr")
	h.serveQR(w, r, code)
}

func (h *QRHandler) GetQR(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	h.serveQR(w, r, code)
}

func (h *QRHandler) serveQR(w http.ResponseWriter, r *http.Request, code string) {

	_, err := h.svc.GetByCode(r.Context(), code)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			jsonError(w, "link not found", http.StatusNotFound)
		case errors.Is(err, service.ErrLinkExpired):
			jsonError(w, "link has expired", http.StatusGone)
		default:
			jsonError(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	size := 256
	if s := r.URL.Query().Get("size"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 && parsed <= 1024 {
			size = parsed
		}
	}

	shortURL := resolveBaseURL(r, h.cfg) + "/" + code
	qr, err := qrcode.New(shortURL, qrcode.Medium)
	if err != nil {
		jsonError(w, "failed to generate QR code", http.StatusInternalServerError)
		return
	}
	qr.DisableBorder = true
	png, err := qr.PNG(size)
	if err != nil {
		jsonError(w, "failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.Write(png)
}
