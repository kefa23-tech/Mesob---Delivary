package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/orders/usecase"
)

type KitchenHandler struct{ service *usecase.KitchenService }

func NewKitchenHandler(service *usecase.KitchenService) *KitchenHandler {
	return &KitchenHandler{service: service}
}
func (h *KitchenHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(restaurantPartnerRequired)
	r.Get("/", h.list)
	r.Post("/{id}/accept", h.accept)
	r.Post("/{id}/reject", h.reject)
	r.Post("/{id}/preparing", h.preparing)
	r.Post("/{id}/ready", h.ready)
	return r
}
func restaurantPartnerRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-User-ID")) == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "restaurant partner authentication required"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (h *KitchenHandler) list(w http.ResponseWriter, r *http.Request) {
	orders, err := h.service.List(r.Context(), r.Header.Get("X-User-ID"))
	if err != nil {
		kitchenError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": orders})
}
func (h *KitchenHandler) accept(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PrepMinutes int `json:"prep_minutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	o, err := h.service.Accept(r.Context(), r.Header.Get("X-User-ID"), chi.URLParam(r, "id"), in.PrepMinutes)
	if err != nil {
		kitchenError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func (h *KitchenHandler) reject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason domain.RestaurantRejectionReason `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.service.Reject(r.Context(), r.Header.Get("X-User-ID"), chi.URLParam(r, "id"), in.Reason); err != nil {
		kitchenError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "REJECTED_BY_RESTAURANT"})
}
func (h *KitchenHandler) preparing(w http.ResponseWriter, r *http.Request) {
	o, err := h.service.Preparing(r.Context(), r.Header.Get("X-User-ID"), chi.URLParam(r, "id"))
	if err != nil {
		kitchenError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func (h *KitchenHandler) ready(w http.ResponseWriter, r *http.Request) {
	o, err := h.service.Ready(r.Context(), r.Header.Get("X-User-ID"), chi.URLParam(r, "id"))
	if err != nil {
		kitchenError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func kitchenError(w http.ResponseWriter, err error) {
	code, status := "order_transition_failed", http.StatusConflict
	if errors.Is(err, domain.ErrInvalidOrder) {
		code, status = "invalid_request", http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
}
