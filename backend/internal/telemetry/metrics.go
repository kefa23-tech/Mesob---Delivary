package telemetry

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/mesob-delivery/backend/internal/platform/redisconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry = prometheus.NewRegistry()

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total", Help: "Total HTTP requests handled by the Mesob API.",
	}, []string{"method", "route", "status"})
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds", Help: "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})
	asynqQueueTasks = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mesob_asynq_queue_tasks", Help: "Current Asynq task count by queue and state.",
	}, []string{"queue", "state"})
	asynqTaskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "mesob_asynq_task_duration_seconds", Help: "Asynq task processing duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"queue", "task_type"})
	dlqTasks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mesob_dlq_tasks_total", Help: "Tasks archived after exhausting retries.",
	}, []string{"queue", "task_type"})
	activeSSE = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mesob_active_sse_connections", Help: "Number of active Server-Sent Events connections.",
	})
	courierLocations = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "mesob_courier_location_updates_total", Help: "Accepted courier GPS location updates.",
	})
	paymentWebhooks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mesob_payment_webhook_total", Help: "Payment webhook events by provider and result.",
	}, []string{"provider", "result"})
	ledgerBalanced = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mesob_ledger_balanced", Help: "1 when the global double-entry ledger is balanced, 0 when not, -1 when the check failed.",
	})
	ledgerBalanceMinor = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mesob_ledger_balance_minor", Help: "Global sum of signed ledger lines in minor currency units; must be zero.",
	})
)

func init() {
	registry.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration, asynqQueueTasks, asynqTaskDuration, dlqTasks,
		activeSSE, courierLocations, paymentWebhooks, ledgerBalanced, ledgerBalanceMinor,
	)
	ledgerBalanced.Set(-1)
}

// Handler returns the Prometheus exposition endpoint for this process.
func Handler() http.Handler { return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}) }

// HTTPMiddleware records low-cardinality route-level request metrics.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		method := strings.ToUpper(r.Method)
		status := strconv.Itoa(wrapped.status)
		httpRequests.WithLabelValues(method, route, status).Inc()
		httpDuration.WithLabelValues(method, route, status).Observe(time.Since(started).Seconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status, w.wrote = status, true
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
func (w *statusWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}
func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// TrackSSEConnection increments the live stream gauge and returns its release function.
func TrackSSEConnection() func() {
	activeSSE.Inc()
	return activeSSE.Dec
}

func RecordCourierLocation() { courierLocations.Inc() }
func RecordPaymentWebhook(provider, result string) {
	paymentWebhooks.WithLabelValues(provider, result).Inc()
}
func RecordArchivedTask(queue, taskType string) { dlqTasks.WithLabelValues(queue, taskType).Inc() }

// AsynqMiddleware measures the runtime of each task, including failed attempts.
func AsynqMiddleware() asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			queue, _ := asynq.GetQueueName(ctx)
			started := time.Now()
			err := next.ProcessTask(ctx, task)
			asynqTaskDuration.WithLabelValues(queue, task.Type()).Observe(time.Since(started).Seconds())
			return err
		})
	}
}

// StartAsynqQueueCollector periodically refreshes pending/active/scheduled/retry/archived/completed gauges.
func StartAsynqQueueCollector(ctx context.Context, redisURL string, logger *slog.Logger) (func() error, error) {
	opt, err := redisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	inspector := asynq.NewInspector(opt)
	lastArchived := make(map[string]int)
	stop := make(chan struct{})
	done := make(chan struct{})
	refresh := func() {
		for _, queue := range []string{"critical", "default", "low", "payments", "dispatch"} {
			info, err := inspector.GetQueueInfo(queue)
			if err != nil {
				logger.Warn("read Asynq queue metrics", "queue", queue, "error", err)
				continue
			}
			if info.Archived > lastArchived[queue] {
				dlqTasks.WithLabelValues(queue, "unknown").Add(float64(info.Archived - lastArchived[queue]))
			}
			lastArchived[queue] = info.Archived
			for state, count := range map[string]int{
				"pending": info.Pending, "active": info.Active, "scheduled": info.Scheduled,
				"retry": info.Retry, "archived": info.Archived, "completed": info.Completed,
			} {
				asynqQueueTasks.WithLabelValues(queue, state).Set(float64(count))
			}
		}
	}
	go func() {
		defer close(done)
		refresh()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
	return func() error {
		close(stop)
		<-done
		return inspector.Close()
	}, nil
}

type ledgerBalanceReader interface {
	GlobalBalance(context.Context) (int64, error)
}

// StartLedgerInvariantCollector publishes the global signed-line sum and a 1/0/-1 integrity gauge.
func StartLedgerInvariantCollector(ctx context.Context, reader ledgerBalanceReader, logger *slog.Logger) {
	refresh := func() {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		balance, err := reader.GlobalBalance(checkCtx)
		if err != nil {
			ledgerBalanced.Set(-1)
			logger.ErrorContext(checkCtx, "ledger invariant check failed", "error", err)
			return
		}
		ledgerBalanceMinor.Set(float64(balance))
		if balance == 0 {
			ledgerBalanced.Set(1)
			return
		}
		ledgerBalanced.Set(0)
		logger.ErrorContext(checkCtx, "global ledger invariant violated", "signed_balance_minor", balance)
	}
	refresh()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}

func redisOptions(raw string) (asynq.RedisConnOpt, error) {
	return redisconfig.ParseAsynqURI(raw)
}
