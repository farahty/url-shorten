package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/farahty/url-shorten/internal/model"
	"github.com/farahty/url-shorten/internal/repository"
	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	repo *repository.LinkRepository
}

func NewAdminHandler(repo *repository.LinkRepository) *AdminHandler {
	return &AdminHandler{repo: repo}
}

func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req model.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.AppName == "" {
		jsonError(w, "app_name is required", http.StatusBadRequest)
		return
	}

	// Generate a random 32-byte API key
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		jsonError(w, "failed to generate API key", http.StatusInternalServerError)
		return
	}
	apiKey := hex.EncodeToString(rawKey)

	// Hash it for storage
	hash := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(hash[:])

	var baseURL *string
	if req.BaseURL != "" {
		baseURL = &req.BaseURL
	}

	key, err := h.repo.CreateAPIKey(r.Context(), keyHash, req.AppName, baseURL)
	if err != nil {
		jsonError(w, "failed to create API key", http.StatusInternalServerError)
		return
	}

	resp := model.CreateAPIKeyResponse{
		ID:        key.ID,
		APIKey:    apiKey,
		AppName:   key.AppName,
		CreatedAt: key.CreatedAt,
	}
	if key.BaseURL != nil {
		resp.BaseURL = *key.BaseURL
	}

	jsonResponse(w, resp, http.StatusCreated)
}

func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.repo.ListAPIKeys(r.Context())
	if err != nil {
		jsonError(w, "failed to list API keys", http.StatusInternalServerError)
		return
	}

	var items []model.APIKeyInfoResponse
	for _, k := range keys {
		info := model.APIKeyInfoResponse{
			ID:        k.ID,
			AppName:   k.AppName,
			IsActive:  k.IsActive,
			CreatedAt: k.CreatedAt,
			UpdatedAt: k.UpdatedAt,
		}
		if k.BaseURL != nil {
			info.BaseURL = *k.BaseURL
		}
		items = append(items, info)
	}

	if items == nil {
		items = []model.APIKeyInfoResponse{}
	}

	jsonResponse(w, items, http.StatusOK)
}

func (h *AdminHandler) DeactivateAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.repo.DeactivateAPIKey(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			jsonError(w, "API key not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to deactivate API key", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
