package http

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
	"github.com/mesob-delivery/backend/internal/catalog/usecase"
	"github.com/mesob-delivery/backend/internal/platform/httpmiddleware"
	"net/http"
	"strconv"
)

type Handler struct{ service *usecase.Service }

func NewHandler(service *usecase.Service) *Handler { return &Handler{service: service} }
func (h *Handler) Routes() chi.Router {
	router := chi.NewRouter()
	router.Get("/", h.listRestaurants)
	return router
}

func (h *Handler) listRestaurants(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	lat, err := strconv.ParseFloat(query.Get("lat"), 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_location", "lat is required and must be numeric")
		return
	}
	lng, err := strconv.ParseFloat(query.Get("lng"), 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_location", "lng is required and must be numeric")
		return
	}
	limit := 20
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_limit", "limit must be an integer")
			return
		}
	}
	cursor, err := usecase.DecodeCursor(query.Get("cursor"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_cursor", "cursor is invalid")
		return
	}
	var eco *bool
	if value := query.Get("eco_packaging"); value != "" {
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_filter", "eco_packaging must be boolean")
			return
		}
		eco = &parsed
	}
	result, err := h.service.Search(r.Context(), domain.SearchFilter{Lat: lat, Lng: lng, Cuisine: query.Get("cuisine"), Query: query.Get("q"), EcoPackaging: eco, Cursor: cursor, Limit: limit})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidSearch) {
			writeError(w, r, http.StatusBadRequest, "invalid_search", err.Error())
			return
		}
		writeError(w, r, http.StatusInternalServerError, "catalog_unavailable", "restaurant discovery is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": httpmiddleware.ID(r.Context())}})
}
