package usecase_test

import (
	"context"
	"errors"
	"github.com/mesob-delivery/backend/internal/auth/adapter/memory"
	"github.com/mesob-delivery/backend/internal/auth/adapter/security"
	"github.com/mesob-delivery/backend/internal/auth/adapter/token"
	"github.com/mesob-delivery/backend/internal/auth/domain"
	"github.com/mesob-delivery/backend/internal/auth/usecase"
	"testing"
	"time"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

type channel struct{}

func (channel) Send(context.Context, string, string) error { return nil }

func service(clock usecase.Clock, store *memory.Store) *usecase.Service {
	return usecase.NewService(usecase.Dependencies{Clock: clock, OTPs: store, Users: store, Limiter: store, Channels: usecase.OTPChannels{SMS: channel{}, WhatsApp: channel{}}, Hasher: security.Argon2id{}, Tokens: token.New("test-secret")})
}

func TestVerifyOTPExpiresAfterFiveMinutes(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	store := memory.New()
	svc := service(clock.Now, store)
	if err := svc.RequestOTP(context.Background(), "+251911000000"); err != nil {
		t.Fatal(err)
	}
	otp, err := store.Find(context.Background(), "+251911000000")
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(5 * time.Minute)
	_, err = svc.VerifyOTP(context.Background(), "+251911000000", otp.Code)
	if !errors.Is(err, domain.ErrInvalidOTP) {
		t.Fatalf("expected expired OTP, got %v", err)
	}
}

func TestVerifyOTPLocksAfterFiveFailures(t *testing.T) {
	clock := &testClock{now: time.Now()}
	store := memory.New()
	svc := service(clock.Now, store)
	if err := svc.RequestOTP(context.Background(), "+251911000001"); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		_, err := svc.VerifyOTP(context.Background(), "+251911000001", "000000")
		if attempt < 5 && !errors.Is(err, domain.ErrInvalidOTP) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if attempt == 5 && !errors.Is(err, domain.ErrRateLimited) {
			t.Fatalf("expected lockout, got %v", err)
		}
	}
	_, err := svc.VerifyOTP(context.Background(), "+251911000001", "000000")
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected locked account, got %v", err)
	}
}
