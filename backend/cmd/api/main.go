package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	paymentclients "github.com/mesob-delivery/backend/internal/adapter/payment"
	"github.com/mesob-delivery/backend/internal/adapter/postgres"
	adminasynq "github.com/mesob-delivery/backend/internal/admin/adapter/asynq"
	adminhttp "github.com/mesob-delivery/backend/internal/admin/adapter/http"
	adminpostgres "github.com/mesob-delivery/backend/internal/admin/adapter/postgres"
	adminusecase "github.com/mesob-delivery/backend/internal/admin/usecase"
	authhttp "github.com/mesob-delivery/backend/internal/auth/adapter/http"
	"github.com/mesob-delivery/backend/internal/auth/adapter/otp"
	authpostgres "github.com/mesob-delivery/backend/internal/auth/adapter/postgres"
	authredis "github.com/mesob-delivery/backend/internal/auth/adapter/redis"
	"github.com/mesob-delivery/backend/internal/auth/adapter/security"
	"github.com/mesob-delivery/backend/internal/auth/adapter/token"
	authusecase "github.com/mesob-delivery/backend/internal/auth/usecase"
	cataloghttp "github.com/mesob-delivery/backend/internal/catalog/adapter/http"
	catalogpostgres "github.com/mesob-delivery/backend/internal/catalog/adapter/postgres"
	catalogredis "github.com/mesob-delivery/backend/internal/catalog/adapter/redis"
	catalogusecase "github.com/mesob-delivery/backend/internal/catalog/usecase"
	dispatchasynq "github.com/mesob-delivery/backend/internal/dispatch/adapter/asynq"
	dispatchhttp "github.com/mesob-delivery/backend/internal/dispatch/adapter/http"
	dispatchpostgres "github.com/mesob-delivery/backend/internal/dispatch/adapter/postgres"
	dispatchredis "github.com/mesob-delivery/backend/internal/dispatch/adapter/redis"
	dispatchusecase "github.com/mesob-delivery/backend/internal/dispatch/usecase"
	healthhttp "github.com/mesob-delivery/backend/internal/health/adapter/http"
	healthusecase "github.com/mesob-delivery/backend/internal/health/usecase"
	ledgerpostgres "github.com/mesob-delivery/backend/internal/ledger/adapter/postgres"
	ledgerusecase "github.com/mesob-delivery/backend/internal/ledger/usecase"
	ordershttp "github.com/mesob-delivery/backend/internal/orders/adapter/http"
	orderspostgres "github.com/mesob-delivery/backend/internal/orders/adapter/postgres"
	ordersredis "github.com/mesob-delivery/backend/internal/orders/adapter/redis"
	ordersusecase "github.com/mesob-delivery/backend/internal/orders/usecase"
	paymentasynq "github.com/mesob-delivery/backend/internal/payment/adapter/asynq"
	paymenthttp "github.com/mesob-delivery/backend/internal/payment/adapter/http"
	paymentpostgres "github.com/mesob-delivery/backend/internal/payment/adapter/postgres"
	paymentdomain "github.com/mesob-delivery/backend/internal/payment/domain"
	paymentusecase "github.com/mesob-delivery/backend/internal/payment/usecase"
	"github.com/mesob-delivery/backend/internal/platform/httpmiddleware"
	realtimeredis "github.com/mesob-delivery/backend/internal/realtime/adapter/redis"
	"github.com/mesob-delivery/backend/internal/telemetry"
)

type config struct {
	Address               string
	DatabaseURL           string
	MigrationsPath        string
	RedisURL              string
	JWTSecret             string
	SMSGatewayURL         string
	WhatsAppGatewayURL    string
	WebAppURL             string
	PublicAPIURL          string
	ChapaAPIURL           string
	ChapaSecretKey        string
	ChapaWebhookSecret    string
	TelebirrAPIURL        string
	TelebirrAPIKey        string
	TelebirrWebhookSecret string
	ProcessRole           string
	CORSOrigins           []string
}

var defaultProcessRole = "api"

const vercelAdminOrigin = "https://mesob-delivery-admin.vercel.app"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	corsOrigins := strings.Split(envOr("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001"), ",")
	corsOrigins = append(corsOrigins, vercelAdminOrigin)
	if err := run(context.Background(), config{
		Address:               ipv4ListenAddress(envOr("API_ADDRESS", ":"+envOr("PORT", "8080"))),
		DatabaseURL:           envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/mesob?sslmode=disable"),
		MigrationsPath:        envOr("MIGRATIONS_PATH", "migrations"),
		RedisURL:              envOr("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:             envOr("JWT_SECRET", "development-only-change-me"),
		SMSGatewayURL:         os.Getenv("SMS_GATEWAY_URL"),
		WhatsAppGatewayURL:    os.Getenv("WHATSAPP_GATEWAY_URL"),
		WebAppURL:             envOr("WEB_APP_URL", "http://localhost:3000/en"),
		PublicAPIURL:          envOr("PUBLIC_API_URL", "http://localhost:8080"),
		ChapaAPIURL:           envOr("CHAPA_API_URL", "https://api.chapa.co/v1"),
		ChapaSecretKey:        os.Getenv("CHAPA_SECRET_KEY"),
		ChapaWebhookSecret:    os.Getenv("CHAPA_WEBHOOK_SECRET"),
		TelebirrAPIURL:        os.Getenv("TELEBIRR_API_URL"),
		TelebirrAPIKey:        os.Getenv("TELEBIRR_API_KEY"),
		TelebirrWebhookSecret: os.Getenv("TELEBIRR_WEBHOOK_SECRET"),
		ProcessRole:           envOr("PROCESS_ROLE", defaultProcessRole),
		CORSOrigins:           corsOrigins,
	}, logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

// ipv4ListenAddress keeps the configured port but binds the API on all IPv4
// interfaces so Railway's IPv4 edge proxy can reach the process.
func ipv4ListenAddress(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		port = strings.TrimPrefix(address, ":")
	}
	return net.JoinHostPort("0.0.0.0", port)
}

func run(parent context.Context, cfg config, logger *slog.Logger) error {
	if dsn := os.Getenv("SENTRY_DSN"); dsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: dsn, Environment: envOr("SENTRY_ENVIRONMENT", "production"), EnableTracing: true, TracesSampleRate: 0.1}); err != nil {
			return fmt.Errorf("initialize Sentry: %w", err)
		}
		defer sentry.Flush(2 * time.Second)
	}
	if cfg.ProcessRole != "api" && cfg.ProcessRole != "worker" {
		return errors.New("PROCESS_ROLE must be api or worker")
	}
	if cfg.ProcessRole == "api" {
		if err := postgres.RunMigrations(parent, cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	profileServer := &http.Server{Addr: envOr("PPROF_ADDRESS", "127.0.0.1:6060"), Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := profileServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Go pprof listener stopped", "address", profileServer.Addr, "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := profileServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("stop Go pprof listener", "error", err)
		}
	}()
	database, err := postgres.Open(parent, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	redisStore, err := authredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "auth Redis store", redisStore.Close)()
	catalogCache, err := catalogredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "catalog Redis cache", catalogCache.Close)()
	catalogRepository := catalogpostgres.NewRepository(database)
	catalogService := catalogusecase.NewServiceWithMenu(catalogRepository, catalogRepository, catalogCache)
	catalogHandler := cataloghttp.NewHandler(catalogService)
	groupCartStore, err := ordersredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "group cart Redis store", groupCartStore.Close)()
	groupCartService := ordersusecase.NewGroupCartService(groupCartStore, orderspostgres.NewRepository(database), 0)
	groupCartHandler := ordershttp.NewHandler(groupCartService, cfg.WebAppURL)
	orderRepository := orderspostgres.NewRepository(database)
	paymentQueue, err := paymentasynq.NewQueue(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "payment task queue", paymentQueue.Close)()
	dispatchRegistry, err := dispatchredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "dispatch Redis registry", dispatchRegistry.Close)()
	dispatchQueue, err := dispatchasynq.NewQueue(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "dispatch task queue", dispatchQueue.Close)()
	realtimeHub, err := realtimeredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "realtime Redis hub", realtimeHub.Close)()
	dispatchService := dispatchusecase.NewService(dispatchpostgres.NewRepository(database), dispatchRegistry, dispatchQueue, dispatchRegistry, realtimeHub)
	dispatchHandler := dispatchhttp.NewHandler(dispatchService, realtimeHub)
	checkoutService := ordersusecase.NewCheckoutService(orderRepository, groupCartStore, groupCartStore, paymentQueue, realtimeHub)
	trackingService := ordersusecase.NewTrackingService(orderRepository, realtimeHub, realtimeHub)
	checkoutHandler := ordershttp.NewCheckoutHandler(checkoutService).WithTracking(ordershttp.NewTrackingHandler(trackingService, cfg.JWTSecret))
	kitchenService := ordersusecase.NewKitchenService(orderRepository, paymentQueue, dispatchQueue, realtimeHub)
	kitchenHandler := ordershttp.NewKitchenHandler(kitchenService, realtimeHub)
	paymentGateways := paymentclients.Registry{
		paymentdomain.Chapa:    paymentclients.NewChapaClient(cfg.ChapaAPIURL, cfg.ChapaSecretKey, nil),
		paymentdomain.Telebirr: paymentclients.NewTelebirrClient(cfg.TelebirrAPIURL, cfg.TelebirrAPIKey, nil),
	}
	ledgerRepository := ledgerpostgres.NewRepository(database)
	ledgerService := ledgerusecase.NewService(ledgerRepository)
	telemetry.StartLedgerInvariantCollector(ctx, ledgerRepository, logger)
	adminService := adminusecase.NewOperations(adminpostgres.NewRepository(database))
	adminHandler := adminhttp.NewHandler(adminService, database)
	dlqInspector, err := adminasynq.NewInspector(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "admin DLQ inspector", dlqInspector.Close)()
	adminHandler.WithDLQ(dlqInspector)
	paymentService := paymentusecase.NewService(paymentpostgres.NewRepository(database, ledgerService), paymentGateways, paymentQueue, cfg.PublicAPIURL, cfg.WebAppURL, realtimeHub)
	paymentHandler := paymenthttp.NewHandler(paymentService, cfg.ChapaWebhookSecret, cfg.TelebirrWebhookSecret)
	stopQueueMetrics, err := telemetry.StartAsynqQueueCollector(ctx, cfg.RedisURL, logger)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "Asynq metrics collector", stopQueueMetrics)()
	if cfg.ProcessRole == "worker" {
		workerDone := make([]<-chan error, 0, 4)
		shutdownWorkers := make([]func(), 0, 4)
		unexpectedExit := make(chan error, 4)
		var shutdownOnce sync.Once
		shutdown := func() {
			shutdownOnce.Do(func() {
				for i := len(shutdownWorkers) - 1; i >= 0; i-- {
					shutdownWorkers[i]()
				}
				for _, done := range workerDone {
					if err := <-done; err != nil {
						logger.Error("background component stopped", "error", err)
					}
				}
			})
		}
		defer shutdown()
		start := func(name string, run func() error, stop func()) {
			done := make(chan error, 1)
			workerDone = append(workerDone, done)
			shutdownWorkers = append(shutdownWorkers, stop)
			go func() {
				err := run()
				done <- err
				if err == nil {
					err = fmt.Errorf("%s stopped unexpectedly", name)
				} else {
					err = fmt.Errorf("%s stopped: %w", name, err)
				}
				unexpectedExit <- err
			}()
		}
		paymentWorker, err := paymentasynq.NewWorker(cfg.RedisURL, paymentService, restaurantPaymentNotifier{logger: logger}, kitchenService, groupCartStore)
		if err != nil {
			return err
		}
		start("payment worker", paymentWorker.Run, paymentWorker.Shutdown)
		paymentScheduler, err := paymentasynq.NewScheduler(cfg.RedisURL)
		if err != nil {
			return err
		}
		start("payment scheduler", paymentScheduler.Run, paymentScheduler.Shutdown)
		dispatchWorker, err := dispatchasynq.NewWorker(cfg.RedisURL, dispatchService)
		if err != nil {
			return err
		}
		start("dispatch worker", dispatchWorker.Run, dispatchWorker.Shutdown)
		dispatchScheduler, err := dispatchasynq.NewScheduler(cfg.RedisURL)
		if err != nil {
			return err
		}
		start("dispatch scheduler", dispatchScheduler.Run, dispatchScheduler.Shutdown)
		logger.Info("background workers started")
		select {
		case <-ctx.Done():
			shutdown()
			return nil
		case err := <-unexpectedExit:
			shutdown()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	users := authpostgres.NewUserRepository(database)
	authService := authusecase.NewService(authusecase.Dependencies{
		OTPs:    redisStore,
		Users:   users,
		Limiter: redisStore,
		Channels: authusecase.OTPChannels{
			SMS:      deliveryProvider(cfg.SMSGatewayURL, "sms", logger),
			WhatsApp: deliveryProvider(cfg.WhatsAppGatewayURL, "whatsapp", logger),
		},
		Hasher: security.Argon2id{},
		Tokens: token.NewWithStore(cfg.JWTSecret, redisStore),
	})
	authHandler := authhttp.NewHandler(authService)

	healthService := healthusecase.NewService(time.Now)
	healthHandler := healthhttp.NewHandler(healthService)

	router := chi.NewRouter()
	drain := httpmiddleware.NewDrainController()
	rateLimiter, err := httpmiddleware.NewRateLimiter(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer closeOnReturn(logger, "HTTP rate limiter", rateLimiter.Close)()
	router.Use(httpmiddleware.SecurityHeaders)
	router.Use(httpmiddleware.CORS(cfg.CORSOrigins))
	router.Use(httpmiddleware.RequestID)
	router.Use(drain.Middleware)
	router.Use(rateLimiter.Middleware)
	router.Use(telemetry.HTTPMiddleware)
	router.Use(sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle)
	router.Get("/metrics", telemetry.Handler().ServeHTTP)
	router.Mount("/api/v1", healthHandler.Routes())
	router.Mount("/api/v1/auth", authHandler.Routes())
	router.Mount("/api/v1/restaurants", catalogHandler.Routes())
	router.Mount("/api/v1/group-carts", groupCartHandler.Routes())
	router.Mount("/api/v1/orders", checkoutHandler.Routes())
	identity := httpmiddleware.AccessTokenIdentity(cfg.JWTSecret)
	router.Mount("/api/v1/dashboard/restaurant/orders", identity(kitchenHandler.Routes()))
	router.Mount("/api/v1/dashboard/restaurant/stream", identity(kitchenHandler.StreamRoutes()))
	router.Mount("/api/v1/courier", identity(dispatchHandler.Routes()))
	router.Mount("/api/v1/payments", paymentHandler.Routes())
	router.Mount("/api/v1/admin", identity(adminHandler.Routes()))

	server := &http.Server{Addr: cfg.Address, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", cfg.Address)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		drain.BeginDrain()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, server.Close())
		}
		return nil
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func closeOnReturn(logger *slog.Logger, resource string, closeFn func() error) func() {
	return func() {
		if err := closeFn(); err != nil {
			logger.Error("failed to close resource", "resource", resource, "error", err)
		}
	}
}

type restaurantPaymentNotifier struct{ logger *slog.Logger }

func (n restaurantPaymentNotifier) RestaurantOrderPaid(ctx context.Context, orderID string) error {
	n.logger.InfoContext(ctx, "restaurant payment notification event", "event", "restaurant.order_paid", "order_id", orderID)
	return nil
}

func deliveryProvider(endpoint, channel string, logger *slog.Logger) authusecase.OTPChannel {
	if endpoint != "" {
		return otp.NewHTTPProvider(endpoint)
	}
	return otp.LoggingProvider{Channel: channel, Logger: logger}
}
