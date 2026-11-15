package asynqadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

const (
	QueueName            = "dispatch"
	FindCourierTask      = "dispatch:find_courier"
	OfferTimeoutTask     = "dispatch:offer_timeout"
	escalateDispatchTask = "dispatch:escalate_stale"
)

type Queue struct{ client *asynq.Client }

func NewQueue(redisURL string) (*Queue, error) {
	opt, err := redisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	return &Queue{client: asynq.NewClient(opt)}, nil
}
func (q *Queue) Close() error { return q.client.Close() }
func (q *Queue) StartDispatch(ctx context.Context, orderID string) error {
	return q.ScheduleFindCourier(ctx, domain.OrderID(orderID), domain.TierOne, 0)
}
func (q *Queue) ScheduleFindCourier(ctx context.Context, orderID domain.OrderID, tier domain.Tier, delay time.Duration) error {
	return q.enqueue(ctx, FindCourierTask, orderID, tier, delay)
}
func (q *Queue) ScheduleOfferTimeout(ctx context.Context, orderID domain.OrderID, tier domain.Tier, delay time.Duration) error {
	return q.enqueue(ctx, OfferTimeoutTask, orderID, tier, delay)
}
func (q *Queue) enqueue(ctx context.Context, taskType string, id domain.OrderID, tier domain.Tier, delay time.Duration) error {
	payload, err := json.Marshal(dispatchPayload{OrderID: id, Tier: tier})
	if err != nil {
		return err
	}
	idempotent := fmt.Sprintf("%s:%s:%d", strings.ReplaceAll(taskType, ":", "-"), id, tier)
	_, err = q.client.EnqueueContext(ctx, asynq.NewTask(taskType, payload), asynq.ProcessIn(delay), asynq.Queue(QueueName), asynq.TaskID(idempotent), asynq.MaxRetry(10), asynq.Timeout(30*time.Second))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "task id") {
		return nil
	}
	return err
}

type dispatchPayload struct {
	OrderID domain.OrderID `json:"order_id"`
	Tier    domain.Tier    `json:"tier"`
}

func redisOptions(raw string) (asynq.RedisClientOpt, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return asynq.RedisClientOpt{}, err
	}
	if u.Host == "" {
		return asynq.RedisClientOpt{}, errors.New("Redis URL must include host")
	}
	opt := asynq.RedisClientOpt{Addr: u.Host}
	if u.User != nil {
		opt.Username = u.User.Username()
		opt.Password, _ = u.User.Password()
	}
	if p := strings.TrimPrefix(u.Path, "/"); p != "" {
		opt.DB, err = strconv.Atoi(p)
		if err != nil {
			return asynq.RedisClientOpt{}, err
		}
	}
	return opt, nil
}
