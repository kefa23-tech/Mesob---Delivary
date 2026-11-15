package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mesob-delivery/backend/internal/payment/domain"
	"github.com/mesob-delivery/backend/internal/realtime"
)

var PollDelays = []time.Duration{time.Minute, 3 * time.Minute, 5 * time.Minute}

type Service struct {
	repository   Repository
	gateways     GatewayRegistry
	queue        Queue
	callbackBase string
	returnBase   string
	publisher    realtime.Publisher
}

func NewService(repository Repository, gateways GatewayRegistry, queue Queue, callbackBase, returnBase string, publishers ...realtime.Publisher) *Service {
	s := &Service{repository: repository, gateways: gateways, queue: queue, callbackBase: strings.TrimRight(callbackBase, "/"), returnBase: strings.TrimRight(returnBase, "/")}
	if len(publishers) > 0 {
		s.publisher = publishers[0]
	}
	return s
}

type InitializeResponse struct {
	OrderID     string          `json:"order_id"`
	Provider    domain.Provider `json:"provider"`
	Reference   string          `json:"reference"`
	CheckoutURL string          `json:"checkout_url"`
}

func (s *Service) Initialize(ctx context.Context, userID, orderID, returnURL string) (InitializeResponse, error) {
	provider, err := s.repositoryProvider(ctx, userID, orderID)
	if err != nil {
		return InitializeResponse{}, err
	}
	gateway, ok := s.gateways.Gateway(provider)
	if !ok {
		return InitializeResponse{}, fmt.Errorf("payment gateway %s is not configured", provider)
	}
	transaction, err := s.repository.CreateIntent(ctx, userID, orderID, provider)
	if err != nil {
		return InitializeResponse{}, err
	}
	if transaction.Status == domain.Paid {
		return InitializeResponse{}, domain.ErrPaymentState
	}
	callback := s.callbackBase + "/api/v1/payments/webhook/" + strings.ToLower(string(provider))
	if returnURL == "" {
		returnURL = s.returnBase + "/payment/return?order_id=" + url.QueryEscape(orderID)
	}
	result, err := gateway.Initialize(ctx, domain.InitializeRequest{
		Reference: transaction.Reference, AmountMinor: transaction.AmountMinor, Currency: transaction.Currency,
		ReturnURL: returnURL, CallbackURL: callback,
		Metadata: map[string]any{"order_id": orderID, "provider": provider, "amount_minor": transaction.AmountMinor},
	})
	if err != nil {
		_, _, _ = s.repository.FailAndRestore(ctx, provider, transaction.Reference)
		return InitializeResponse{}, err
	}
	if result.Reference != transaction.Reference {
		_, _, _ = s.repository.FailAndRestore(ctx, provider, transaction.Reference)
		return InitializeResponse{}, domain.ErrInvalidPayment
	}
	if err := s.repository.UpdateInitialized(ctx, transaction.Reference, result); err != nil {
		return InitializeResponse{}, err
	}
	if s.queue != nil {
		for attempt, delay := range PollDelays {
			if err := s.queue.SchedulePoll(ctx, provider, transaction.Reference, attempt+1, delay); err != nil {
				return InitializeResponse{}, fmt.Errorf("schedule payment status poll %d: %w", attempt+1, err)
			}
		}
	}
	return InitializeResponse{OrderID: orderID, Provider: provider, Reference: transaction.Reference, CheckoutURL: result.CheckoutURL}, nil
}

// HandleWebhook expects the HTTP adapter to verify the raw-body signature first.
func (s *Service) HandleWebhook(ctx context.Context, provider domain.Provider, raw []byte) error {
	var event struct {
		Reference      string      `json:"tx_ref"`
		OrderReference string      `json:"order_reference"`
		Status         string      `json:"status"`
		TradeStatus    string      `json:"trade_status"`
		Amount         json.Number `json:"amount"`
		AmountMinor    int64       `json:"amount_minor"`
		Currency       string      `json:"currency"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return domain.ErrInvalidPayment
	}
	if event.Reference == "" {
		event.Reference = event.OrderReference
	}
	if event.Reference == "" {
		return domain.ErrInvalidPayment
	}
	if event.Status == "" {
		event.Status = event.TradeStatus
	}
	gateway, ok := s.gateways.Gateway(provider)
	if !ok {
		return fmt.Errorf("payment gateway %s is not configured", provider)
	}
	verification, err := gateway.Verify(ctx, event.Reference)
	if err != nil {
		return err
	}
	if verification.Reference != event.Reference {
		return domain.ErrInvalidPayment
	}
	if verification.Status == domain.Paid {
		orderID, transitioned, err := s.repository.Confirm(ctx, provider, event.Reference, verification.AmountMinor, verification.Currency)
		if err != nil {
			return err
		}
		if transitioned {
			s.afterPaid(ctx, provider, event.Reference, orderID)
		}
		return nil
	}
	if verification.Status == domain.Failed {
		orderID, transitioned, err := s.repository.FailAndRestore(ctx, provider, event.Reference)
		if err == nil && transitioned {
			s.publishCancelled(ctx, orderID)
			if s.queue != nil {
				_ = s.queue.CancelPolling(ctx, provider, event.Reference)
			}
		}
		return err
	}
	return errors.New("payment is not confirmed by gateway")
}

func (s *Service) Poll(ctx context.Context, provider domain.Provider, reference string, attempt int) error {
	transaction, err := s.repository.GetTransaction(ctx, provider, reference)
	if err != nil {
		return err
	}
	if transaction.Status != domain.Pending {
		return nil
	}
	gateway, ok := s.gateways.Gateway(provider)
	if !ok {
		return fmt.Errorf("payment gateway %s is not configured", provider)
	}
	verification, err := gateway.Verify(ctx, reference)
	if err != nil {
		return err
	}
	if verification.Reference != reference {
		return domain.ErrInvalidPayment
	}
	switch verification.Status {
	case domain.Paid:
		orderID, transitioned, err := s.repository.Confirm(ctx, provider, reference, verification.AmountMinor, verification.Currency)
		if err != nil {
			return err
		}
		if transitioned {
			s.afterPaid(ctx, provider, reference, orderID)
		}
	case domain.Failed:
		orderID, transitioned, err := s.repository.FailAndRestore(ctx, provider, reference)
		if err != nil {
			return err
		}
		if transitioned {
			s.publishCancelled(ctx, orderID)
			if s.queue != nil {
				_ = s.queue.CancelPolling(ctx, provider, reference)
			}
		}
	default:
		if attempt >= len(PollDelays) {
			orderID, transitioned, err := s.repository.FailAndRestore(ctx, provider, reference)
			if err != nil {
				return err
			}
			if transitioned {
				s.publishCancelled(ctx, orderID)
				if s.queue != nil {
					_ = s.queue.CancelPolling(ctx, provider, reference)
				}
			}
		}
	}
	return nil
}

func (s *Service) afterPaid(ctx context.Context, provider domain.Provider, reference, orderID string) {
	s.publishStatus(ctx, orderID, "PAID")
	if s.queue == nil {
		return
	}
	_ = s.queue.CancelPolling(ctx, provider, reference)
	_ = s.queue.NotifyRestaurantPaid(ctx, orderID)
	if scheduler, ok := s.queue.(interface {
		ScheduleRestaurantSLA(context.Context, string, time.Duration) error
	}); ok {
		_ = scheduler.ScheduleRestaurantSLA(ctx, orderID, 5*time.Minute)
	}
}

func (s *Service) publishCancelled(ctx context.Context, orderID string) {
	s.publishStatus(ctx, orderID, "cancelled")
}
func (s *Service) publishStatus(ctx context.Context, orderID, status string) {
	if s.publisher != nil {
		event := realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: orderID, Status: status}
		if resolver, ok := s.repository.(interface {
			RestaurantID(context.Context, string) (string, error)
		}); ok {
			event.RestaurantID, _ = resolver.RestaurantID(ctx, orderID)
		}
		_ = s.publisher.Publish(ctx, event)
	}
}

// RepositoryProvider keeps order ownership and allowed payment-method checks in the persistence adapter.
type providerResolver interface {
	ResolveProvider(context.Context, string, string) (domain.Provider, error)
}

func (s *Service) repositoryProvider(ctx context.Context, userID, orderID string) (domain.Provider, error) {
	resolver, ok := s.repository.(providerResolver)
	if !ok {
		return "", errors.New("payment repository cannot resolve order payment method")
	}
	return resolver.ResolveProvider(ctx, userID, orderID)
}
