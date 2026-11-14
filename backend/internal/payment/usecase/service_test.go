package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	paymentdomain "github.com/mesob-delivery/backend/internal/payment/domain"
	"github.com/mesob-delivery/backend/internal/payment/usecase"
)

type fakeRepository struct {
	transaction        paymentdomain.Transaction
	confirms, failures int
	transitioned       bool
}

func (r *fakeRepository) ResolveProvider(context.Context, string, string) (paymentdomain.Provider, error) {
	return r.transaction.Provider, nil
}
func (r *fakeRepository) CreateIntent(context.Context, string, string, paymentdomain.Provider) (paymentdomain.Transaction, error) {
	return r.transaction, nil
}
func (r *fakeRepository) UpdateInitialized(context.Context, string, paymentdomain.InitializeResult) error {
	return nil
}
func (r *fakeRepository) GetTransaction(context.Context, paymentdomain.Provider, string) (paymentdomain.Transaction, error) {
	return r.transaction, nil
}
func (r *fakeRepository) Confirm(_ context.Context, _ paymentdomain.Provider, _ string, amount int64, currency string) (string, bool, error) {
	if amount != r.transaction.AmountMinor || currency != r.transaction.Currency {
		return "", false, paymentdomain.ErrInvalidPayment
	}
	r.confirms++
	was := r.transitioned
	r.transitioned = true
	r.transaction.Status = paymentdomain.Paid
	return r.transaction.OrderID, !was, nil
}
func (r *fakeRepository) FailAndRestore(context.Context, paymentdomain.Provider, string) (string, bool, error) {
	r.failures++
	r.transaction.Status = paymentdomain.Failed
	return r.transaction.OrderID, true, nil
}

type fakeGateway struct {
	result paymentdomain.Verification
	calls  int
}

func (g *fakeGateway) Initialize(_ context.Context, in paymentdomain.InitializeRequest) (paymentdomain.InitializeResult, error) {
	return paymentdomain.InitializeResult{Reference: in.Reference, CheckoutURL: "https://checkout.test"}, nil
}
func (g *fakeGateway) Verify(_ context.Context, ref string) (paymentdomain.Verification, error) {
	g.calls++
	result := g.result
	result.Reference = ref
	return result, nil
}

type fakeGateways map[paymentdomain.Provider]paymentdomain.Gateway

func (g fakeGateways) Gateway(p paymentdomain.Provider) (paymentdomain.Gateway, bool) {
	v, ok := g[p]
	return v, ok
}

type fakeQueue struct {
	polls                        []int
	cancellations, notifications int
}

func (q *fakeQueue) SchedulePoll(_ context.Context, _ paymentdomain.Provider, _ string, attempt int, delay time.Duration) error {
	q.polls = append(q.polls, attempt)
	return nil
}
func (q *fakeQueue) CancelPolling(context.Context, paymentdomain.Provider, string) error {
	q.cancellations++
	return nil
}
func (q *fakeQueue) NotifyRestaurantPaid(context.Context, string) error {
	q.notifications++
	return nil
}

func TestWebhookConfirmationIsIdempotent(t *testing.T) {
	repo := &fakeRepository{transaction: paymentdomain.Transaction{OrderID: "order-1", Provider: paymentdomain.Chapa, Reference: "order-1", AmountMinor: 15100, Currency: "ETB", Status: paymentdomain.Pending}}
	gateway := &fakeGateway{result: paymentdomain.Verification{Status: paymentdomain.Paid, AmountMinor: 15100, Currency: "ETB"}}
	queue := &fakeQueue{}
	service := usecase.NewService(repo, fakeGateways{paymentdomain.Chapa: gateway}, queue, "https://api.test", "https://web.test")
	body, _ := json.Marshal(map[string]any{"tx_ref": "order-1", "status": "success"})
	for i := 0; i < 2; i++ {
		if err := service.HandleWebhook(context.Background(), paymentdomain.Chapa, body); err != nil {
			t.Fatal(err)
		}
	}
	if repo.confirms != 2 || queue.notifications != 1 || queue.cancellations != 1 {
		t.Fatalf("confirm=%d notifications=%d cancellations=%d", repo.confirms, queue.notifications, queue.cancellations)
	}
}

func TestPollingFailsAndRestoresOnlyOnThirdScheduledAttempt(t *testing.T) {
	repo := &fakeRepository{transaction: paymentdomain.Transaction{OrderID: "order-2", Provider: paymentdomain.Telebirr, Reference: "order-2", AmountMinor: 17000, Currency: "ETB", Status: paymentdomain.Pending}}
	gateway := &fakeGateway{result: paymentdomain.Verification{Status: paymentdomain.Pending}}
	queue := &fakeQueue{}
	service := usecase.NewService(repo, fakeGateways{paymentdomain.Telebirr: gateway}, queue, "https://api.test", "https://web.test")
	for attempt := 1; attempt <= 2; attempt++ {
		if err := service.Poll(context.Background(), paymentdomain.Telebirr, "order-2", attempt); err != nil {
			t.Fatal(err)
		}
	}
	if repo.failures != 0 {
		t.Fatalf("failed early after %d poll calls", repo.failures)
	}
	if err := service.Poll(context.Background(), paymentdomain.Telebirr, "order-2", 3); err != nil {
		t.Fatal(err)
	}
	if repo.failures != 1 || queue.cancellations != 1 {
		t.Fatalf("failures=%d cancellations=%d", repo.failures, queue.cancellations)
	}
}

func TestPollingPaidPaymentCancelsRemainingTasksAndEmitsNotification(t *testing.T) {
	repo := &fakeRepository{transaction: paymentdomain.Transaction{OrderID: "order-3", Provider: paymentdomain.Chapa, Reference: "order-3", AmountMinor: 25000, Currency: "ETB", Status: paymentdomain.Pending}}
	gateway := &fakeGateway{result: paymentdomain.Verification{Status: paymentdomain.Paid, AmountMinor: 25000, Currency: "ETB"}}
	queue := &fakeQueue{}
	service := usecase.NewService(repo, fakeGateways{paymentdomain.Chapa: gateway}, queue, "https://api.test", "https://web.test")
	if err := service.Poll(context.Background(), paymentdomain.Chapa, "order-3", 1); err != nil {
		t.Fatal(err)
	}
	if queue.cancellations != 1 || queue.notifications != 1 {
		t.Fatalf("cancellations=%d notifications=%d", queue.cancellations, queue.notifications)
	}
}

func TestWebhookRejectsAmountMismatch(t *testing.T) {
	repo := &fakeRepository{transaction: paymentdomain.Transaction{OrderID: "order-4", Provider: paymentdomain.Chapa, Reference: "order-4", AmountMinor: 30000, Currency: "ETB", Status: paymentdomain.Pending}}
	gateway := &fakeGateway{result: paymentdomain.Verification{Status: paymentdomain.Paid, AmountMinor: 29999, Currency: "ETB"}}
	service := usecase.NewService(repo, fakeGateways{paymentdomain.Chapa: gateway}, &fakeQueue{}, "https://api.test", "https://web.test")
	body := []byte(`{"tx_ref":"order-4","status":"success"}`)
	if err := service.HandleWebhook(context.Background(), paymentdomain.Chapa, body); !errors.Is(err, paymentdomain.ErrInvalidPayment) {
		t.Fatalf("expected amount mismatch error, got %v", err)
	}
}
