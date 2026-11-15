package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/telemetry"
)

func (h *KitchenHandler) StreamRoutes() chi.Router {
	r := chi.NewRouter()
	r.Use(restaurantPartnerRequired)
	r.Get("/", h.stream)
	return r
}

func (h *KitchenHandler) stream(w http.ResponseWriter, r *http.Request) {
	releaseMetric := telemetry.TrackSSEConnection()
	defer releaseMetric()
	if h.realtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "restaurant event stream unavailable"})
		return
	}
	ownerID := r.Header.Get("X-User-ID")
	restaurantID, err := h.service.RestaurantID(r.Context(), ownerID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "restaurant not found"})
		return
	}
	events, err := h.realtime.SubscribeRestaurant(r.Context(), restaurantID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "restaurant event stream unavailable"})
		return
	}
	orders, err := h.service.List(r.Context(), ownerID)
	if err != nil {
		kitchenError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if writeKitchenSSE(w, "snapshot", map[string]any{"data": orders}) != nil {
		return
	}
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			if writeKitchenSSE(w, string(event.Type), event) != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
func writeKitchenSSE(w http.ResponseWriter, name string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload)
	return err
}
