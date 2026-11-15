package httpmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAccessTokenIdentityUsesSignedSubject(t *testing.T) {
	secret := "test-secret"
	claims := jwt.MapClaims{"sub": "verified-user", "exp": time.Now().Add(time.Minute).Unix()}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	handler := AccessTokenIdentity(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("X-User-ID") }))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-User-ID", "spoofed-user")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || got != "verified-user" {
		t.Fatalf("status=%d identity=%q", response.Code, got)
	}
}

func TestAccessTokenIdentityRejectsMissingOrInvalidToken(t *testing.T) {
	handler := AccessTokenIdentity("test-secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler should not be called") }))
	for _, authorization := range []string{"", "Bearer invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("authorization %q: status=%d", authorization, response.Code)
		}
	}
}
