package http_test

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	authhttp "github.com/mesob-delivery/backend/internal/auth/adapter/http"
	"github.com/mesob-delivery/backend/internal/auth/adapter/memory"
	"github.com/mesob-delivery/backend/internal/auth/adapter/security"
	"github.com/mesob-delivery/backend/internal/auth/adapter/token"
	"github.com/mesob-delivery/backend/internal/auth/usecase"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type channel struct{}

func (channel) Send(context.Context, string, string) error { return nil }
func newRouter() (*chi.Mux, *memory.Store) {
	store := memory.New()
	svc := usecase.NewService(usecase.Dependencies{OTPs: store, Users: store, Limiter: store, Channels: usecase.OTPChannels{SMS: channel{}, WhatsApp: channel{}}, Hasher: security.Argon2id{}, Tokens: token.New("test-secret")})
	router := chi.NewRouter()
	router.Mount("/auth", authhttp.NewHandler(svc).Routes())
	return router, store
}

func TestRefreshRotatesRefreshToken(t *testing.T) {
	router, store := newRouter()
	request := httptest.NewRequest(nethttp.MethodPost, "/auth/otp/request", strings.NewReader(`{"phone":"+251911000002"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != nethttp.StatusAccepted {
		t.Fatalf("request OTP: %d", response.Code)
	}
	otp, err := store.Find(context.Background(), "+251911000002")
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(nethttp.MethodPost, "/auth/otp/verify", strings.NewReader(`{"phone":"+251911000002","code":"`+otp.Code+`"}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != nethttp.StatusOK {
		t.Fatalf("verify OTP: %d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "mesob_refresh" {
		t.Fatal("expected httpOnly refresh cookie")
	}
	oldCookie := cookies[0]
	request = httptest.NewRequest(nethttp.MethodPost, "/auth/refresh", nil)
	request.AddCookie(oldCookie)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != nethttp.StatusOK {
		t.Fatalf("refresh: %d", response.Code)
	}
	newCookie := response.Result().Cookies()[0]
	if newCookie.Value == oldCookie.Value {
		t.Fatal("refresh token was not rotated")
	}
	request = httptest.NewRequest(nethttp.MethodPost, "/auth/refresh", nil)
	request.AddCookie(oldCookie)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != nethttp.StatusUnauthorized {
		t.Fatalf("expected old token rejection, got %d", response.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(response.Body).Decode(&body)
}
