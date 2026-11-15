package asynqadapter

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/hibiken/asynq"
)

type Scheduler struct{ scheduler *asynq.Scheduler }

func NewScheduler(redisURL string) (*Scheduler, error) {
	opt, err := schedulerRedisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	scheduler := asynq.NewScheduler(opt, nil)
	if _, err := scheduler.Register("*/5 * * * *", asynq.NewTask(reconcilePaymentsTaskType, nil), asynq.Queue(QueueName)); err != nil {
		return nil, fmt.Errorf("register orphaned payment reconciler: %w", err)
	}
	if _, err := scheduler.Register("*/5 * * * *", asynq.NewTask(cleanupEphemeralTaskType, nil), asynq.Queue(QueueName)); err != nil {
		return nil, fmt.Errorf("register ephemeral lock cleanup: %w", err)
	}
	return &Scheduler{scheduler: scheduler}, nil
}
func (s *Scheduler) Run() error { return s.scheduler.Run() }
func (s *Scheduler) Shutdown()  { s.scheduler.Shutdown() }
func schedulerRedisOptions(raw string) (asynq.RedisClientOpt, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return asynq.RedisClientOpt{}, err
	}
	if u.Host == "" {
		return asynq.RedisClientOpt{}, fmt.Errorf("Redis URL must include host")
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
