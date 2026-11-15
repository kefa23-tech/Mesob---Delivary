package workerobs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/hibiken/asynq"
)

var archivedTasks atomic.Uint64

// ErrorHandler logs failed tasks and counts attempts that exhaust their retry budget.
func ErrorHandler(logger *slog.Logger) asynq.ErrorHandler {
	return asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
		queue, _ := asynq.GetQueueName(ctx)
		id, _ := asynq.GetTaskID(ctx)
		retries, _ := asynq.GetRetryCount(ctx)
		maxRetries, _ := asynq.GetMaxRetry(ctx)
		logger.ErrorContext(ctx, "background task failed", "queue", queue, "task_id", id, "task_type", task.Type(), "retry_count", retries, "max_retries", maxRetries, "error", err)
		if retries >= maxRetries {
			archivedTasks.Add(1)
			logger.ErrorContext(ctx, "background task archived", "queue", queue, "task_id", id, "task_type", task.Type(), "retry_count", retries, "error", err)
		}
	})
}

// MetricsHandler exposes the DLQ counter in Prometheus text exposition format.
func MetricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP mesob_dlq_tasks_total Tasks archived after exhausting retries.\n# TYPE mesob_dlq_tasks_total counter\nmesob_dlq_tasks_total %d\n", archivedTasks.Load())
}
