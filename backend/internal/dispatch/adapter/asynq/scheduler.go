package asynqadapter

import (
	"fmt"

	"github.com/hibiken/asynq"
)

type Scheduler struct{ scheduler *asynq.Scheduler }

func NewScheduler(redisURL string) (*Scheduler, error) {
	opt, err := redisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	scheduler := asynq.NewScheduler(opt, nil)
	if _, err := scheduler.Register("* * * * *", asynq.NewTask(escalateDispatchTask, nil), asynq.Queue(QueueName)); err != nil {
		return nil, fmt.Errorf("register stale dispatch escalator: %w", err)
	}
	return &Scheduler{scheduler: scheduler}, nil
}
func (s *Scheduler) Run() error { return s.scheduler.Run() }
func (s *Scheduler) Shutdown()  { s.scheduler.Shutdown() }
