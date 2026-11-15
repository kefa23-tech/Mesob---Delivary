package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	paymentclients "github.com/mesob-delivery/backend/internal/adapter/payment"
	"github.com/mesob-delivery/backend/internal/adapter/postgres"
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
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(context.Background(), config{
		Address:               envOr("API_ADDRESS", ":8080"),
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
	}, logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context, cfg config, logger *slog.Logger) error {
	if err := postgres.RunMigrations(parent, cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
		return err
	}
	database, err := postgres.Open(parent, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	redisStore, err := authredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer redisStore.Close()
	catalogCache, err := catalogredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer catalogCache.Close()
	catalogRepository := catalogpostgres.NewRepository(database)
	catalogService := catalogusecase.NewServiceWithMenu(catalogRepository, catalogRepository, catalogCache)
	catalogHandler := cataloghttp.NewHandler(catalogService)
	groupCartStore, err := ordersredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer groupCartStore.Close()
	groupCartService := ordersusecase.NewGroupCartService(groupCartStore, orderspostgres.NewRepository(database), 0)
	groupCartHandler := ordershttp.NewHandler(groupCartService, cfg.WebAppURL)
	orderRepository := orderspostgres.NewRepository(database)
	paymentQueue, err := paymentasynq.NewQueue(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer paymentQueue.Close()
	dispatchRegistry, err := dispatchredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer dispatchRegistry.Close()
	dispatchQueue, err := dispatchasynq.NewQueue(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer dispatchQueue.Close()
	realtimeHub, err := realtimeredis.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer realtimeHub.Close()
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
	adminService := adminusecase.NewOperations(adminpostgres.NewRepository(database))
	adminHandler := adminhttp.NewHandler(adminService, database)
	paymentService := paymentusecase.NewService(paymentpostgres.NewRepository(database, ledgerService), paymentGateways, paymentQueue, cfg.PublicAPIURL, cfg.WebAppURL, realtimeHub)
	paymentHandler := paymenthttp.NewHandler(paymentService, cfg.ChapaWebhookSecret, cfg.TelebirrWebhookSecret)
	paymentWorker, err := paymentasynq.NewWorker(cfg.RedisURL, paymentService, restaurantPaymentNotifier{logger: logger}, kitchenService)
	if err != nil {
		return err
	}
	defer paymentWorker.Shutdown()
	go func() {
		if err := paymentWorker.Run(); err != nil {
			logger.Error("payment worker stopped", "error", err)
		}
	}()
	dispatchWorker, err := dispatchasynq.NewWorker(cfg.RedisURL, dispatchService)
	if err != nil {
		return err
	}
	defer dispatchWorker.Shutdown()
	go func() {
		if err := dispatchWorker.Run(); err != nil {
			logger.Error("dispatch worker stopped", "error", err)
		}
	}()
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
	router.Use(httpmiddleware.RequestID)
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
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
