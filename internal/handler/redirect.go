package handler

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"

	"github.com/farahty/url-shorten/internal/config"
	"github.com/farahty/url-shorten/internal/middleware"
	"github.com/farahty/url-shorten/internal/repository"
	"github.com/farahty/url-shorten/internal/service"
	"github.com/go-chi/chi/v5"
)

var ogTemplate = template.Must(template.New("og").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta property="og:title" content="{{.Title}}" />
  <meta property="og:description" content="{{.Description}}" />
  <meta property="og:image" content="{{.Image}}" />
  <meta property="og:url" content="{{.ShortURL}}" />
  <meta property="og:site_name" content="{{.SiteName}}" />
  <meta http-equiv="refresh" content="0;url={{.OriginalURL}}" />
</head>
<body>
  Redirecting to <a href="{{.OriginalURL}}">{{.OriginalURL}}</a>
</body>
</html>`))

type ogTemplateData struct {
	Title       string
	Description string
	Image       string
	ShortURL    string
	SiteName    string
	OriginalURL string
}

type RedirectHandler struct {
	svc *service.LinkService
	cfg *config.Config
}

func NewRedirectHandler(svc *service.LinkService, cfg *config.Config) *RedirectHandler {
	return &RedirectHandler{svc: svc, cfg: cfg}
}

func (h *RedirectHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	isCrawler, _ := r.Context().Value(middleware.IsCrawlerKey).(bool)

	if isCrawler {
		h.serveCrawler(w, r, code)
		return
	}

	url, err := h.svc.Resolve(r.Context(), code)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, "404 not found", http.StatusNotFound)
		case errors.Is(err, service.ErrLinkExpired):
			http.Error(w, "410 gone", http.StatusGone)
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
}

func (h *RedirectHandler) serveCrawler(w http.ResponseWriter, r *http.Request, code string) {
	link, err := h.svc.ResolveForCrawler(r.Context(), code)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, "404 not found", http.StatusNotFound)
		case errors.Is(err, service.ErrLinkExpired):
			http.Error(w, "410 gone", http.StatusGone)
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	baseURL := h.cfg.BaseURL
	if link.AppBaseURL != nil && *link.AppBaseURL != "" {
		baseURL = *link.AppBaseURL
	}

	data := ogTemplateData{
		ShortURL:    fmt.Sprintf("%s/%s", baseURL, code),
		OriginalURL: link.OriginalURL,
	}

	if link.OGTitle != nil {
		data.Title = *link.OGTitle
	}
	if link.OGDesc != nil {
		data.Description = *link.OGDesc
	}
	if link.OGImage != nil {
		data.Image = *link.OGImage
	}
	if link.OGSite != nil {
		data.SiteName = *link.OGSite
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	ogTemplate.Execute(w, data)
}
