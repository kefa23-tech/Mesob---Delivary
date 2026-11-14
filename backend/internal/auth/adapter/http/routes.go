package http

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/auth/domain"
	"github.com/mesob-delivery/backend/internal/auth/usecase"
	"net/http"
)

type Handler struct{ service *usecase.Service }

func NewHandler(service *usecase.Service) *Handler { return &Handler{service: service} }
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/otp/request", h.requestOTP)
	r.Post("/otp/verify", h.verifyOTP)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	return r
}

type otpRequest struct {
	Phone   string `json:"phone"`
	Channel string `json:"channel"`
}
type otpVerifyRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type refreshResponse struct {
	Session domain.Session `json:"session"`
}

func (h *Handler) requestOTP(w http.ResponseWriter, r *http.Request) {
	var in otpRequest
	if !decode(w, r, &in) {
		return
	}
	if err := h.service.RequestOTPWithChannel(r.Context(), in.Phone, in.Channel); err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "OTP sent"})
}
func (h *Handler) verifyOTP(w http.ResponseWriter, r *http.Request) {
	var in otpVerifyRequest
	if !decode(w, r, &in) {
		return
	}
	session, err := h.service.VerifyOTP(r.Context(), in.Phone, in.Code)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, session)
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if !decode(w, r, &in) {
		return
	}
	session, err := h.service.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, session)
}
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("mesob_refresh")
	if err != nil {
		writeAuthError(w, domain.ErrInvalidCredentials)
		return
	}
	session, err := h.service.Refresh(r.Context(), cookie.Value)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, refreshResponse{Session: session})
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("mesob_refresh"); err == nil {
		_ = h.service.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "mesob_refresh", Value: "", Path: "/api/v1/auth", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusNoContent, nil)
}
func setRefreshCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{Name: "mesob_refresh", Value: value, Path: "/api/v1/auth", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 60 * 60})
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "bad_request", "message": "invalid JSON"})
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func writeAuthError(w http.ResponseWriter, err error) {
	status, code := http.StatusUnauthorized, "unauthorized"
	switch {
	case errors.Is(err, domain.ErrRateLimited):
		status, code = http.StatusTooManyRequests, "otp_locked"
	case errors.Is(err, domain.ErrInvalidOTP):
		status, code = http.StatusUnauthorized, "invalid_otp"
	case errors.Is(err, domain.ErrInvalidCredentials):
		status, code = http.StatusUnauthorized, "invalid_credentials"
	}
	writeJSON(w, status, map[string]string{"code": code, "message": err.Error()})
}
