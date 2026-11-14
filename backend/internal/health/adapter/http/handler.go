package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/health/usecase"
	"github.com/mesob-delivery/backend/internal/platform/httpmiddleware"
)

type Handler struct{ health usecase.HealthChecker }

func NewHandler(health usecase.HealthChecker) *Handler { return &Handler{health: health} }

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/health", h.check)
	return r
}

func (h *Handler) check(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		RequestID string      `json:"request_id"`
		Data      interface{} `json:"data"`
	}{RequestID: httpmiddleware.ID(r.Context()), Data: h.health.Check(r.Context())})
}
