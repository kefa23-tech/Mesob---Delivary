package asynqadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/mesob-delivery/backend/internal/admin/domain"
)

var queues = []string{"critical", "default", "low", "payments", "dispatch"}

// Inspector implements the admin application port over Asynq's archived queues.
type Inspector struct{ inspector *asynq.Inspector }

func NewInspector(redisURL string) (*Inspector, error) {
	opt, err := parseRedisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	return &Inspector{inspector: asynq.NewInspector(opt)}, nil
}
func (i *Inspector) Close() error { return i.inspector.Close() }
func (i *Inspector) List(_ context.Context) ([]domain.DeadLetterTask, map[string]int64, error) {
	result := make([]domain.DeadLetterTask, 0)
	depths := make(map[string]int64, len(queues))
	for _, queue := range queues {
		info, err := i.inspector.GetQueueInfo(queue)
		if err != nil {
			if errors.Is(err, asynq.ErrQueueNotFound) {
				depths[queue] = 0
				continue
			}
			return nil, nil, fmt.Errorf("inspect queue %s: %w", queue, err)
		}
		depths[queue] = int64(info.Size)
		items, err := i.inspector.ListArchivedTasks(queue, asynq.PageSize(100), asynq.Page(1))
		if err != nil {
			return nil, nil, fmt.Errorf("list archived tasks in %s: %w", queue, err)
		}
		for _, task := range items {
			payload := string(task.Payload)
			summary := strings.TrimSpace(payload)
			if len(summary) > 180 {
				summary = summary[:180] + "…"
			}
			if json.Valid(task.Payload) {
				var value any
				_ = json.Unmarshal(task.Payload, &value)
				if obj, ok := value.(map[string]any); ok {
					if id, ok := obj["order_id"].(string); ok {
						summary = "order_id=" + id
					}
				}
			}
			result = append(result, domain.DeadLetterTask{ID: task.ID, Queue: task.Queue, Type: task.Type, Payload: payload, PayloadSummary: summary, LastError: task.LastErr, RetryCount: task.Retried, LastFailedAt: task.LastFailedAt})
		}
	}
	return result, depths, nil
}
func (i *Inspector) Retry(_ context.Context, queue, id string) error {
	if !valid(queue, id) {
		return errors.New("invalid queue or task id")
	}
	return i.inspector.RunTask(queue, id)
}
func (i *Inspector) Delete(_ context.Context, queue, id string) error {
	if !valid(queue, id) {
		return errors.New("invalid queue or task id")
	}
	return i.inspector.DeleteTask(queue, id)
}
func valid(queue, id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for _, allowed := range queues {
		if queue == allowed {
			return true
		}
	}
	return false
}

func parseRedisOptions(raw string) (asynq.RedisClientOpt, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return asynq.RedisClientOpt{}, fmt.Errorf("parse Asynq Redis URL: %w", err)
	}
	if u.Host == "" {
		return asynq.RedisClientOpt{}, errors.New("redis URL must include host")
	}
	opt := asynq.RedisClientOpt{Addr: u.Host}
	if u.User != nil {
		opt.Username = u.User.Username()
		opt.Password, _ = u.User.Password()
	}
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		opt.DB, err = strconv.Atoi(path)
		if err != nil {
			return asynq.RedisClientOpt{}, fmt.Errorf("invalid Redis database: %w", err)
		}
	}
	return opt, nil
}
