package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/dispatch/domain"
	"github.com/mesob-delivery/backend/internal/dispatch/usecase"
)

type Handler struct{ service *usecase.Service }

func NewHandler(service *usecase.Service) *Handler { return &Handler{service: service} }
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(requireCourier)
	r.Post("/status", h.status)
	r.Post("/location", h.location)
	r.Get("/offers", h.offers)
	r.Get("/offers/stream", h.offerStream)
	r.Post("/offers/{order_id}/accept", h.accept)
	r.Post("/deliveries/{order_id}/pickup", h.pickup)
	r.Post("/deliveries/{order_id}/complete", h.complete)
	return r
}
func requireCourier(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-User-ID")) == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "courier authentication required"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Online  bool               `json:"online"`
		Vehicle domain.VehicleType `json:"vehicle_type"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.service.SetStatus(r.Context(), domain.CourierID(r.Header.Get("X-User-ID")), in.Online, in.Vehicle); err != nil {
		dispatchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": in.Online, "vehicle_type": in.Vehicle})
}
func (h *Handler) location(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Latitude  float64 `json:"lat"`
		Longitude float64 `json:"lng"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.service.SetLocation(r.Context(), domain.CourierID(r.Header.Get("X-User-ID")), in.Latitude, in.Longitude); err != nil {
		dispatchError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "location_updated"})
}
func (h *Handler) offers(w http.ResponseWriter, r *http.Request) {
	orders, err := h.service.Offers(r.Context(), domain.CourierID(r.Header.Get("X-User-ID")))
	if err != nil {
		dispatchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": orders})
}
func (h *Handler) offerStream(w http.ResponseWriter, r *http.Request) {
	stream, err := h.service.OfferStream(r.Context(), domain.CourierID(r.Header.Get("X-User-ID")))
	if err != nil {
		dispatchError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case offer, open := <-stream:
			if !open {
				return
			}
			data, err := json.Marshal(offer)
			if err != nil {
				continue
			}
			if _, err = fmt.Fprintf(w, "event: offer\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Claim(r.Context(), domain.OrderID(chi.URLParam(r, "order_id")), domain.CourierID(r.Header.Get("X-User-ID"))); err != nil {
		dispatchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "COURIER_ASSIGNED"})
}
func (h *Handler) pickup(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Pickup(r.Context(), domain.OrderID(chi.URLParam(r, "order_id")), domain.CourierID(r.Header.Get("X-User-ID"))); err != nil {
		dispatchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "PICKED_UP"})
}
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PIN string `json:"pin"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.service.Complete(r.Context(), domain.OrderID(chi.URLParam(r, "order_id")), domain.CourierID(r.Header.Get("X-User-ID")), in.PIN); err != nil {
		dispatchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "DELIVERED"})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_request", "message": "invalid JSON body"}})
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func dispatchError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "dispatch_failed"
	switch {
	case errors.Is(err, domain.ErrInvalidCourier), errors.Is(err, domain.ErrInvalidPIN):
		status, code = http.StatusUnprocessableEntity, "invalid_request"
	case errors.Is(err, domain.ErrCourierIneligible):
		status, code = http.StatusForbidden, "courier_ineligible"
	case errors.Is(err, domain.ErrOfferExpired):
		status, code = http.StatusGone, "offer_expired"
	case errors.Is(err, domain.ErrOrderClaimed), errors.Is(err, domain.ErrOrderNotAssignable), errors.Is(err, domain.ErrCourierCODLimit), errors.Is(err, domain.ErrInvalidOrder):
		status, code = http.StatusConflict, "order_unavailable"
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
}
