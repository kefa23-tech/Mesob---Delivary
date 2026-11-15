package http

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	paymentclient "github.com/mesob-delivery/backend/internal/adapter/payment"
	"github.com/mesob-delivery/backend/internal/payment/domain"
	"github.com/mesob-delivery/backend/internal/payment/usecase"
	"github.com/mesob-delivery/backend/internal/telemetry"
)

type Handler struct {
	service        *usecase.Service
	chapaSecret    string
	telebirrSecret string
}

func NewHandler(service *usecase.Service, chapaSecret, telebirrSecret string) *Handler {
	return &Handler{service: service, chapaSecret: chapaSecret, telebirrSecret: telebirrSecret}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/webhook/chapa", h.chapaWebhook)
	r.Post("/webhook/telebirr", h.telebirrWebhook)
	r.Post("/{orderID}/initialize", h.initialize)
	return r
}

func (h *Handler) initialize(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ReturnURL string `json:"return_url"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
	}
	if strings.TrimSpace(r.Header.Get("X-User-ID")) == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "X-User-ID is required")
		return
	}
	result, err := h.service.Initialize(r.Context(), r.Header.Get("X-User-ID"), chi.URLParam(r, "orderID"), input.ReturnURL)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "payment_initialization_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) chapaWebhook(w http.ResponseWriter, r *http.Request) {
	h.webhook(w, r, domain.Chapa, h.chapaSecret, []string{"x-chapa-signature", "chapa-signature"})
}
func (h *Handler) telebirrWebhook(w http.ResponseWriter, r *http.Request) {
	h.webhook(w, r, domain.Telebirr, h.telebirrSecret, []string{"x-telebirr-signature", "x-signature"})
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request, provider domain.Provider, secret string, headerNames []string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read webhook body")
		return
	}
	valid := false
	for _, name := range headerNames {
		if paymentclient.VerifyHMACSignature(body, r.Header.Get(name), secret) {
			valid = true
			break
		}
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "webhook signature is invalid")
		return
	}
	if err := h.service.HandleWebhook(r.Context(), provider, body); err != nil {
		telemetry.RecordPaymentWebhook(string(provider), "error")
		writeError(w, http.StatusUnprocessableEntity, "payment_event_rejected", err.Error())
		return
	}
	telemetry.RecordPaymentWebhook(string(provider), "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
