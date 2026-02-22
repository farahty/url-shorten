package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/farahty/url-shorten/internal/config"
	"github.com/farahty/url-shorten/internal/middleware"
	"github.com/farahty/url-shorten/internal/model"
	"github.com/farahty/url-shorten/internal/repository"
	"github.com/farahty/url-shorten/internal/service"
	"github.com/go-chi/chi/v5"
)

type LinkHandler struct {
	svc *service.LinkService
	cfg *config.Config
}

func NewLinkHandler(svc *service.LinkService, cfg *config.Config) *LinkHandler {
	return &LinkHandler{svc: svc, cfg: cfg}
}

func resolveBaseURL(r *http.Request, cfg *config.Config) string {
	if baseURL, ok := r.Context().Value(middleware.AppBaseURLKey).(string); ok && baseURL != "" {
		return baseURL
	}
	return cfg.BaseURL
}

func (h *LinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	apiKeyID, ok := r.Context().Value(middleware.APIKeyIDKey).(string)
	if !ok || apiKeyID == "" {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10*1024) // 10KB limit
	var req model.CreateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		jsonError(w, "url is required", http.StatusBadRequest)
		return
	}

	link, err := h.svc.Create(r.Context(), req, apiKeyID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidURL):
			jsonError(w, "invalid URL", http.StatusBadRequest)
		case errors.Is(err, service.ErrInvalidAlias):
			jsonError(w, "invalid alias: must be 1-32 alphanumeric/hyphen/underscore characters and not a reserved word", http.StatusBadRequest)
		case errors.Is(err, service.ErrAliasConflict):
			jsonError(w, "alias already taken", http.StatusConflict)
		case errors.Is(err, service.ErrExpiryTooLong):
			jsonError(w, "expires_in exceeds maximum of 1 year (31536000 seconds)", http.StatusBadRequest)
		default:
			jsonError(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	baseURL := resolveBaseURL(r, h.cfg)
	resp := model.CreateLinkResponse{
		Code:        link.Code,
		ShortURL:    baseURL + "/" + link.Code,
		OriginalURL: link.OriginalURL,
		ExpiresAt:   link.ExpiresAt,
		QRURL:       "/api/v1/links/" + link.Code + "/qr",
		CreatedAt:   link.CreatedAt,
	}

	if link.OGTitle != nil || link.OGDesc != nil || link.OGImage != nil || link.OGSite != nil {
		resp.OG = &model.OGData{}
		if link.OGTitle != nil {
			resp.OG.Title = *link.OGTitle
		}
		if link.OGDesc != nil {
			resp.OG.Description = *link.OGDesc
		}
		if link.OGImage != nil {
			resp.OG.Image = *link.OGImage
		}
		if link.OGSite != nil {
			resp.OG.SiteName = *link.OGSite
		}
	}

	jsonResponse(w, resp, http.StatusCreated)
}

func (h *LinkHandler) Get(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	link, err := h.svc.GetByCode(r.Context(), code)
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

	resp := model.LinkInfoResponse{
		Code:        link.Code,
		ShortURL:    resolveBaseURL(r, h.cfg) + "/" + link.Code,
		OriginalURL: link.OriginalURL,
		ClickCount:  link.ClickCount,
		IsAlias:     link.IsAlias,
		ExpiresAt:   link.ExpiresAt,
		CreatedAt:   link.CreatedAt,
	}

	jsonResponse(w, resp, http.StatusOK)
}

func (h *LinkHandler) List(w http.ResponseWriter, r *http.Request) {
	apiKeyID, ok := r.Context().Value(middleware.APIKeyIDKey).(string)
	if !ok || apiKeyID == "" {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	// Validate before calling service
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	links, total, err := h.svc.List(r.Context(), apiKeyID, page, limit)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	baseURL := resolveBaseURL(r, h.cfg)
	var items []model.LinkInfoResponse
	for _, l := range links {
		items = append(items, model.LinkInfoResponse{
			Code:        l.Code,
			ShortURL:    baseURL + "/" + l.Code,
			OriginalURL: l.OriginalURL,
			ClickCount:  l.ClickCount,
			IsAlias:     l.IsAlias,
			ExpiresAt:   l.ExpiresAt,
			CreatedAt:   l.CreatedAt,
		})
	}

	if items == nil {
		items = []model.LinkInfoResponse{}
	}

	jsonResponse(w, model.ListLinksResponse{
		Links: items,
		Total: total,
		Page:  page,
		Limit: limit,
	}, http.StatusOK)
}

func (h *LinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	apiKeyID, ok := r.Context().Value(middleware.APIKeyIDKey).(string)
	if !ok || apiKeyID == "" {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	err := h.svc.Delete(r.Context(), code, apiKeyID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			jsonError(w, "link not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("error encoding JSON error response: %v", err)
	}
}
