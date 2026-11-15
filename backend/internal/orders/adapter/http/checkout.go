package http

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/orders/usecase"
)

type CheckoutHandler struct{ service *usecase.CheckoutService }

func NewCheckoutHandler(service *usecase.CheckoutService) *CheckoutHandler {
	return &CheckoutHandler{service: service}
}

func (h *CheckoutHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.place)
	return r
}

func (h *CheckoutHandler) place(w http.ResponseWriter, r *http.Request) {
	var request domain.CheckoutRequest
	if !decode(w, r, &request) {
		return
	}
	if request.AddressID == "" || request.PaymentMethod == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_order", "message": domain.ErrInvalidOrder.Error()}})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "idempotency_key_required", "message": "Idempotency-Key header is required"}})
		return
	}
	body, _, err := h.service.Place(r.Context(), r.Header.Get("X-User-ID"), key, request)
	if err != nil {
		statusCode := http.StatusInternalServerError
		code := "checkout_failed"
		switch {
		case errors.Is(err, domain.ErrInvalidOrder), errors.Is(err, domain.ErrInvalidQuantity), errors.Is(err, domain.ErrInvalidSelection), errors.Is(err, domain.ErrRestaurantMismatch):
			statusCode, code = http.StatusBadRequest, "invalid_order"
		case errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, domain.ErrCartLocked):
			statusCode, code = http.StatusConflict, "checkout_conflict"
		case errors.Is(err, domain.ErrAddressNotFound), errors.Is(err, domain.ErrCartNotFound):
			statusCode, code = http.StatusNotFound, "resource_not_found"
		case errors.Is(err, domain.ErrAddressOutOfRange), errors.Is(err, domain.ErrRestaurantClosed), errors.Is(err, domain.ErrItemUnavailable), errors.Is(err, domain.ErrInsufficientStock):
			statusCode, code = http.StatusUnprocessableEntity, "checkout_unavailable"
		case errors.Is(err, domain.ErrCODOrderLimit), errors.Is(err, domain.ErrCODFirstOrder), errors.Is(err, domain.ErrCODUnpaidCancellation), errors.Is(err, domain.ErrCODSuspended):
			statusCode, code = http.StatusUnprocessableEntity, "cod_ineligible"
		}
		writeJSON(w, statusCode, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, string(body))
}
