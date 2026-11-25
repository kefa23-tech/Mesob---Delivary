package asynqadapter

import (
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/mesob-delivery/backend/internal/platform/redisconfig"
)

type Scheduler struct{ scheduler *asynq.Scheduler }

func NewScheduler(redisURL string) (*Scheduler, error) {
	opt, err := schedulerRedisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	scheduler := asynq.NewScheduler(opt, &asynq.SchedulerOpts{HeartbeatInterval: 30 * time.Second})
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
func schedulerRedisOptions(raw string) (asynq.RedisConnOpt, error) {
	return redisconfig.ParseAsynqURI(raw)
}
