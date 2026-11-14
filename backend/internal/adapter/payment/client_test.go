package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mesob-delivery/backend/internal/payment/domain"
)

func TestVerifyHMACSignature(t *testing.T) {
	body := []byte(`{"event":"payment.success","tx_ref":"order-1"}`)
	mac := hmacSHA256(body, "webhook-secret")
	if !VerifyHMACSignature(body, mac, "webhook-secret") {
		t.Fatal("valid HMAC rejected")
	}
	if VerifyHMACSignature(body, mac, "wrong-secret") {
		t.Fatal("signature accepted with wrong secret")
	}
	if VerifyHMACSignature(append(body, '!'), mac, "webhook-secret") {
		t.Fatal("signature accepted for changed raw body")
	}
}

func TestChapaInitializeUsesETBUnitsAndCopiesReturnMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transaction/initialize" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer chapa-secret" {
			t.Fatalf("authorization header missing")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["amount"] != "150.25" || request["tx_ref"] != "order-1" || request["return_url"] != "https://web.test/return" {
			t.Fatalf("unexpected payload: %#v", request)
		}
		if request["meta"].(map[string]any)["order_id"] != "order-1" {
			t.Fatalf("metadata missing: %#v", request["meta"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"checkout_url":"https://checkout.test/pay","tx_ref":"order-1"}}`))
	}))
	defer server.Close()
	client := NewChapaClient(server.URL, "chapa-secret", server.Client())
	result, err := client.Initialize(context.Background(), domain.InitializeRequest{Reference: "order-1", AmountMinor: 15025, Currency: "ETB", ReturnURL: "https://web.test/return", CallbackURL: "https://api.test/hook", Metadata: map[string]any{"order_id": "order-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reference != "order-1" || result.CheckoutURL != "https://checkout.test/pay" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestChapaVerifyConvertsMajorUnitsToMinorUnits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/verify/order-2") {
			t.Fatalf("unexpected verify URL %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"status":"success","tx_ref":"order-2","amount":"99.10","currency":"ETB"}}`))
	}))
	defer server.Close()
	result, err := NewChapaClient(server.URL, "secret", server.Client()).Verify(context.Background(), "order-2")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.Paid || result.AmountMinor != 9910 || result.Currency != "ETB" {
		t.Fatalf("unexpected verification: %+v", result)
	}
}

func hmacSHA256(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
