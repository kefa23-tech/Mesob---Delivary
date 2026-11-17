package asynqadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/mesob-delivery/backend/internal/payment/domain"
	"github.com/mesob-delivery/backend/internal/platform/redisconfig"
	"github.com/mesob-delivery/backend/internal/platform/workerobs"
	"github.com/mesob-delivery/backend/internal/telemetry"
)

const (
	QueueName                 = "payments"
	pollTaskType              = "mesob:payment:poll"
	notifyTaskType            = "mesob:restaurant:order_paid"
	restaurantSLATaskType     = "order:restaurant_sla_timeout"
	reconcilePaymentsTaskType = "payment:reconcile_orphaned"
	cleanupEphemeralTaskType  = "ops:cleanup_ephemeral_locks"
)

type Queue struct {
	client    *asynq.Client
	inspector *asynq.Inspector
}

func NewQueue(redisURL string) (*Queue, error) {
	opt, err := redisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	return &Queue{client: asynq.NewClient(opt), inspector: asynq.NewInspector(opt)}, nil
}
func (q *Queue) Close() error {
	return errors.Join(q.inspector.Close(), q.client.Close())
}

func (q *Queue) SchedulePoll(ctx context.Context, provider domain.Provider, reference string, attempt int, delay time.Duration) error {
	payload, err := json.Marshal(pollPayload{Provider: provider, Reference: reference, Attempt: attempt})
	if err != nil {
		return err
	}
	id := pollTaskID(provider, reference, attempt)
	_, err = q.client.EnqueueContext(ctx, asynq.NewTask(pollTaskType, payload), asynq.ProcessIn(delay), asynq.Queue(QueueName), asynq.TaskID(id), asynq.MaxRetry(2), asynq.Timeout(30*time.Second))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "task id") {
		return nil
	}
	return err
}
func (q *Queue) CancelPolling(_ context.Context, provider domain.Provider, reference string) error {
	var result error
	for attempt := 1; attempt <= 3; attempt++ {
		err := q.inspector.DeleteTask(QueueName, pollTaskID(provider, reference, attempt))
		if err != nil && !errors.Is(err, asynq.ErrTaskNotFound) {
			result = errors.Join(result, err)
		}
	}
	return result
}
func (q *Queue) NotifyRestaurantPaid(ctx context.Context, orderID string) error {
	payload, err := json.Marshal(map[string]string{"order_id": orderID})
	if err != nil {
		return err
	}
	_, err = q.client.EnqueueContext(ctx, asynq.NewTask(notifyTaskType, payload), asynq.Queue(QueueName), asynq.TaskID("restaurant-order-paid:"+orderID), asynq.MaxRetry(10))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "task id") {
		return nil
	}
	return err
}

func (q *Queue) ScheduleRestaurantSLA(ctx context.Context, orderID string, delay time.Duration) error {
	if delay <= 0 {
		delay = 5 * time.Minute
	}
	payload, err := json.Marshal(map[string]string{"order_id": orderID})
	if err != nil {
		return err
	}
	_, err = q.client.EnqueueContext(ctx, asynq.NewTask(restaurantSLATaskType, payload), asynq.ProcessIn(delay), asynq.Queue(QueueName), asynq.TaskID("restaurant-sla:"+orderID), asynq.MaxRetry(10))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "task id") {
		return nil
	}
	return err
}
func (q *Queue) CancelRestaurantSLA(_ context.Context, orderID string) error {
	err := q.inspector.DeleteTask(QueueName, "restaurant-sla:"+orderID)
	if errors.Is(err, asynq.ErrTaskNotFound) {
		return nil
	}
	return err
}

type pollPayload struct {
	Provider  domain.Provider `json:"provider"`
	Reference string          `json:"reference"`
	Attempt   int             `json:"attempt"`
}
type poller interface {
	Poll(context.Context, domain.Provider, string, int) error
}
type notifier interface {
	RestaurantOrderPaid(context.Context, string) error
}
type slaTimeoutHandler interface {
	HandleSLATimeout(context.Context, string) error
}
type orphanedPaymentReconciler interface{ ReconcileOrphanedPayments(context.Context) error }
type ephemeralLockCleaner interface{ CleanupEphemeralLocks(context.Context) error }

type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

func NewWorker(redisURL string, paymentPoller poller, notifications notifier, handlers ...any) (*Worker, error) {
	opt, err := redisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	mux := asynq.NewServeMux()
	mux.HandleFunc(pollTaskType, func(ctx context.Context, task *asynq.Task) error {
		var p pollPayload
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			return fmt.Errorf("decode payment poll task: %w", err)
		}
		if !p.Provider.Valid() || p.Reference == "" || p.Attempt < 1 || p.Attempt > 3 {
			return errors.New("invalid payment poll task")
		}
		return paymentPoller.Poll(ctx, p.Provider, p.Reference, p.Attempt)
	})
	mux.HandleFunc(notifyTaskType, func(ctx context.Context, task *asynq.Task) error {
		var payload struct {
			OrderID string `json:"order_id"`
		}
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return err
		}
		if payload.OrderID == "" {
			return errors.New("restaurant notification task has no order id")
		}
		return notifications.RestaurantOrderPaid(ctx, payload.OrderID)
	})
	for _, handler := range handlers {
		if slaHandler, ok := handler.(slaTimeoutHandler); ok {
			mux.HandleFunc(restaurantSLATaskType, func(ctx context.Context, task *asynq.Task) error {
				var p struct {
					OrderID string `json:"order_id"`
				}
				if err := json.Unmarshal(task.Payload(), &p); err != nil {
					return err
				}
				if p.OrderID == "" {
					return errors.New("restaurant SLA task has no order id")
				}
				return slaHandler.HandleSLATimeout(ctx, p.OrderID)
			})
		}
		if cleaner, ok := handler.(ephemeralLockCleaner); ok {
			mux.HandleFunc(cleanupEphemeralTaskType, func(ctx context.Context, _ *asynq.Task) error { return cleaner.CleanupEphemeralLocks(ctx) })
		}
	}
	if reconciler, ok := paymentPoller.(orphanedPaymentReconciler); ok {
		mux.HandleFunc(reconcilePaymentsTaskType, func(ctx context.Context, _ *asynq.Task) error { return reconciler.ReconcileOrphanedPayments(ctx) })
	}
	server := asynq.NewServer(opt, asynq.Config{
		Concurrency:              8,
		Queues:                   map[string]int{QueueName: 5, "default": 1},
		ErrorHandler:             workerobs.ErrorHandler(slog.Default()),
		TaskCheckInterval:        5 * time.Second,
		DelayedTaskCheckInterval: 15 * time.Second,
		HealthCheckInterval:      30 * time.Second,
	})
	mux.Use(telemetry.AsynqMiddleware())
	return &Worker{server: server, mux: mux}, nil
}
func (w *Worker) Run() error { return w.server.Run(w.mux) }
func (w *Worker) Shutdown()  { w.server.Shutdown() }

func redisOptions(raw string) (asynq.RedisConnOpt, error) {
	return redisconfig.ParseAsynqURI(raw)
}
func pollTaskID(provider domain.Provider, reference string, attempt int) string {
	return fmt.Sprintf("payment-poll:%s:%s:%d", strings.ToLower(string(provider)), reference, attempt)
}
