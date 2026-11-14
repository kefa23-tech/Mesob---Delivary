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

	"github.com/mesob-delivery/backend/internal/adapter/postgres"
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
	healthhttp "github.com/mesob-delivery/backend/internal/health/adapter/http"
	healthusecase "github.com/mesob-delivery/backend/internal/health/usecase"
	ordershttp "github.com/mesob-delivery/backend/internal/orders/adapter/http"
	orderspostgres "github.com/mesob-delivery/backend/internal/orders/adapter/postgres"
	ordersredis "github.com/mesob-delivery/backend/internal/orders/adapter/redis"
	ordersusecase "github.com/mesob-delivery/backend/internal/orders/usecase"
	"github.com/mesob-delivery/backend/internal/platform/httpmiddleware"
)

type config struct {
	Address            string
	DatabaseURL        string
	MigrationsPath     string
	RedisURL           string
	JWTSecret          string
	SMSGatewayURL      string
	WhatsAppGatewayURL string
	WebAppURL          string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(context.Background(), config{
		Address:            envOr("API_ADDRESS", ":8080"),
		DatabaseURL:        envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/mesob?sslmode=disable"),
		MigrationsPath:     envOr("MIGRATIONS_PATH", "migrations"),
		RedisURL:           envOr("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:          envOr("JWT_SECRET", "development-only-change-me"),
		SMSGatewayURL:      os.Getenv("SMS_GATEWAY_URL"),
		WhatsAppGatewayURL: os.Getenv("WHATSAPP_GATEWAY_URL"),
		WebAppURL:          envOr("WEB_APP_URL", "http://localhost:3000/en"),
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
	checkoutService := ordersusecase.NewCheckoutService(orderRepository, groupCartStore, groupCartStore)
	checkoutHandler := ordershttp.NewCheckoutHandler(checkoutService)
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

func deliveryProvider(endpoint, channel string, logger *slog.Logger) authusecase.OTPChannel {
	if endpoint != "" {
		return otp.NewHTTPProvider(endpoint)
	}
	return otp.LoggingProvider{Channel: channel, Logger: logger}
}
