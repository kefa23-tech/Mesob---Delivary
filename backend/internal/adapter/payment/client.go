package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mesob-delivery/backend/internal/payment/domain"
)

const defaultChapaAPI = "https://api.chapa.co/v1"

type ChapaClient struct {
	baseURL string
	secret  string
	client  *http.Client
}

func NewChapaClient(baseURL, secret string, client *http.Client) *ChapaClient {
	if baseURL == "" {
		baseURL = defaultChapaAPI
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &ChapaClient{baseURL: strings.TrimRight(baseURL, "/"), secret: secret, client: client}
}

func (c *ChapaClient) Initialize(ctx context.Context, in domain.InitializeRequest) (domain.InitializeResult, error) {
	payload := map[string]any{
		"tx_ref": in.Reference, "amount": minorAsMajor(in.AmountMinor), "currency": in.Currency,
		"return_url": in.ReturnURL, "callback_url": in.CallbackURL, "meta": in.Metadata,
	}
	var response struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    struct {
			CheckoutURL string `json:"checkout_url"`
			Reference   string `json:"tx_ref"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/transaction/initialize", payload, &response); err != nil {
		return domain.InitializeResult{}, err
	}
	if response.Status != "success" || response.Data.CheckoutURL == "" {
		return domain.InitializeResult{}, fmt.Errorf("chapa initialization rejected: %s", response.Message)
	}
	ref := response.Data.Reference
	if ref == "" {
		ref = in.Reference
	}
	return domain.InitializeResult{Reference: ref, CheckoutURL: response.Data.CheckoutURL, GatewayReference: ref}, nil
}

func (c *ChapaClient) Verify(ctx context.Context, reference string) (domain.Verification, error) {
	var response struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Status    string      `json:"status"`
			Reference string      `json:"tx_ref"`
			Amount    json.Number `json:"amount"`
			Currency  string      `json:"currency"`
		} `json:"data"`
	}
	endpoint := c.baseURL + "/transaction/verify/" + url.PathEscape(reference)
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return domain.Verification{}, err
	}
	amount, err := majorAsMinor(string(response.Data.Amount))
	if err != nil {
		return domain.Verification{}, fmt.Errorf("parse Chapa amount: %w", err)
	}
	ref := response.Data.Reference
	if ref == "" {
		ref = reference
	}
	return domain.Verification{Reference: ref, Status: statusFromProvider(response.Data.Status, response.Status), AmountMinor: amount, Currency: response.Data.Currency}, nil
}

type TelebirrClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewTelebirrClient points at the merchant's configured Telebirr API adapter.
// Its transport contract is intentionally configurable because Ethio Telecom
// provisions different API hosts and credentials per merchant environment.
func NewTelebirrClient(baseURL, apiKey string, client *http.Client) *TelebirrClient {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &TelebirrClient{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client}
}

func (c *TelebirrClient) Initialize(ctx context.Context, in domain.InitializeRequest) (domain.InitializeResult, error) {
	payload := map[string]any{
		"order_reference": in.Reference, "amount_minor": in.AmountMinor, "currency": in.Currency,
		"return_url": in.ReturnURL, "notify_url": in.CallbackURL, "metadata": in.Metadata,
	}
	var response struct {
		Status           string `json:"status"`
		Message          string `json:"message"`
		Reference        string `json:"order_reference"`
		GatewayReference string `json:"transaction_id"`
		CheckoutURL      string `json:"checkout_url"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/transactions/initialize", payload, &response); err != nil {
		return domain.InitializeResult{}, err
	}
	if response.Status != "success" || response.CheckoutURL == "" {
		return domain.InitializeResult{}, fmt.Errorf("telebirr initialization rejected: %s", response.Message)
	}
	if response.Reference == "" {
		response.Reference = in.Reference
	}
	return domain.InitializeResult{Reference: response.Reference, CheckoutURL: response.CheckoutURL, GatewayReference: response.GatewayReference}, nil
}

func (c *TelebirrClient) Verify(ctx context.Context, reference string) (domain.Verification, error) {
	var response struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Reference   string `json:"order_reference"`
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/transactions/"+url.PathEscape(reference), nil, &response); err != nil {
		return domain.Verification{}, err
	}
	ref := response.Reference
	if ref == "" {
		ref = reference
	}
	return domain.Verification{Reference: ref, Status: statusFromProvider(response.Status, response.Status), AmountMinor: response.AmountMinor, Currency: response.Currency}, nil
}

func (c *ChapaClient) doJSON(ctx context.Context, method, endpoint string, payload, target any) error {
	return doJSON(ctx, c.client, method, endpoint, c.secret, payload, target)
}
func (c *TelebirrClient) doJSON(ctx context.Context, method, endpoint string, payload, target any) error {
	return doJSON(ctx, c.client, method, endpoint, c.apiKey, payload, target)
}

func doJSON(ctx context.Context, client *http.Client, method, endpoint, credential string, payload, target any) (retErr error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close payment gateway response body: %w", err))
		}
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("payment gateway returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode payment gateway response: %w", err)
	}
	return nil
}

func statusFromProvider(providerStatus, envelopeStatus string) domain.Status {
	s := strings.ToLower(strings.TrimSpace(providerStatus))
	if s == "" {
		s = strings.ToLower(strings.TrimSpace(envelopeStatus))
	}
	switch s {
	case "success", "successful", "paid", "completed", "pay_success":
		return domain.Paid
	case "failed", "failure", "cancelled", "canceled", "expired", "timeout", "pay_failed", "order_closed":
		return domain.Failed
	default:
		return domain.Pending
	}
}

func minorAsMajor(minor int64) string { return fmt.Sprintf("%d.%02d", minor/100, minor%100) }
func majorAsMinor(major string) (int64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(major), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid major currency amount")
	}
	return int64(math.Round(value * 100)), nil
}

// VerifyHMACSignature accepts the hex or standard base64 SHA-256 digest of the
// exact request body. Constant-time comparison prevents timing-based forgery.
func VerifyHMACSignature(body []byte, signature, secret string) bool {
	if len(secret) == 0 || strings.TrimSpace(signature) == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	digest := mac.Sum(nil)
	received := strings.TrimSpace(signature)
	if decoded, err := hex.DecodeString(received); err == nil {
		return hmac.Equal(digest, decoded)
	}
	decoded, err := base64.StdEncoding.DecodeString(received)
	return err == nil && hmac.Equal(digest, decoded)
}
