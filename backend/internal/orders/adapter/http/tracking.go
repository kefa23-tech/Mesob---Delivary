package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/orders/usecase"
)

type TrackingHandler struct {
	service *usecase.TrackingService
	secret  []byte
}

func NewTrackingHandler(service *usecase.TrackingService, jwtSecret string) *TrackingHandler {
	return &TrackingHandler{service: service, secret: []byte(jwtSecret)}
}
func (h *TrackingHandler) Register(r chi.Router) {
	r.Get("/{id}", h.snapshot)
	r.Get("/{id}/stream", h.stream)
}
func (h *TrackingHandler) userID(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return h.secret, nil
	})
	if err != nil || !token.Valid {
		return ""
	}
	role, _ := claims["role"].(string)
	sub, _ := claims.GetSubject()
	if role != "customer" {
		return ""
	}
	return sub
}
func (h *TrackingHandler) snapshot(w http.ResponseWriter, r *http.Request) {
	userID := h.userID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required"})
		return
	}
	s, err := h.service.Snapshot(r.Context(), userID, chi.URLParam(r, "id"))
	if errors.Is(err, domain.ErrOrderNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "order not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "tracking unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, s)
}
func (h *TrackingHandler) stream(w http.ResponseWriter, r *http.Request) {
	userID := h.userID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	snapshot, events, cancel, err := h.service.Connect(r.Context(), userID, chi.URLParam(r, "id"))
	if errors.Is(err, domain.ErrOrderNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "order not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "stream unavailable"})
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := writeSSE(w, "snapshot", snapshot); err != nil {
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
			if writeSSE(w, string(event.Type), event) != nil {
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
func writeSSE(w http.ResponseWriter, event string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	return err
}
