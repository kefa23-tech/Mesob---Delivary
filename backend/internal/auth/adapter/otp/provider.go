package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type LoggingProvider struct {
	Channel string
	Logger  *slog.Logger
}

func (p LoggingProvider) Send(_ context.Context, destination, code string) error {
	p.Logger.Info("OTP delivery requested", "channel", p.Channel, "destination", destination, "code", code)
	return nil
}

type HTTPProvider struct {
	Endpoint string
	Client   *http.Client
}

func NewHTTPProvider(endpoint string) *HTTPProvider {
	return &HTTPProvider{Endpoint: endpoint, Client: &http.Client{Timeout: 5 * time.Second}}
}

func (p *HTTPProvider) Send(ctx context.Context, destination, code string) error {
	body, err := json.Marshal(map[string]string{"destination": destination, "code": code})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OTP gateway returned status %d", response.StatusCode)
	}
	return nil
}
