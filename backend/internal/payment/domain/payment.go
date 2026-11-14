package domain

import (
	"context"
	"errors"
)

var (
	ErrPaymentNotFound = errors.New("payment not found")
	ErrPaymentState    = errors.New("payment is not payable")
	ErrInvalidPayment  = errors.New("payment verification failed")
)

type Provider string

const (
	Chapa    Provider = "CHAPA"
	Telebirr Provider = "TELEBIRR"
)

func (p Provider) Valid() bool { return p == Chapa || p == Telebirr }

type Status string

const (
	Pending Status = "pending"
	Paid    Status = "paid"
	Failed  Status = "failed"
)

type InitializeRequest struct {
	Reference   string         `json:"reference"`
	AmountMinor int64          `json:"amount_minor"`
	Currency    string         `json:"currency"`
	ReturnURL   string         `json:"return_url"`
	CallbackURL string         `json:"callback_url"`
	Metadata    map[string]any `json:"metadata"`
}

type InitializeResult struct {
	Reference        string `json:"reference"`
	CheckoutURL      string `json:"checkout_url"`
	GatewayReference string `json:"gateway_reference,omitempty"`
}

type Verification struct {
	Reference   string
	Status      Status
	AmountMinor int64
	Currency    string
}

type Gateway interface {
	Initialize(context.Context, InitializeRequest) (InitializeResult, error)
	Verify(context.Context, string) (Verification, error)
}

type Transaction struct {
	OrderID     string
	Provider    Provider
	Reference   string
	AmountMinor int64
	Currency    string
	Status      Status
	CheckoutURL string
}
