package usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/mesob-delivery/backend/internal/auth/domain"
	"math/big"
	"strings"
	"time"
)

const (
	OTPValidity   = 5 * time.Minute
	LockoutWindow = 15 * time.Minute
)

type Service struct {
	clock    Clock
	otps     OTPStore
	users    UserRepository
	limiter  RateLimiter
	channels OTPChannels
	hasher   PasswordHasher
	tokens   TokenService
}
type Dependencies struct {
	Clock    Clock
	OTPs     OTPStore
	Users    UserRepository
	Limiter  RateLimiter
	Channels OTPChannels
	Hasher   PasswordHasher
	Tokens   TokenService
}

func NewService(deps Dependencies) *Service {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &Service{deps.Clock, deps.OTPs, deps.Users, deps.Limiter, deps.Channels, deps.Hasher, deps.Tokens}
}

func (s *Service) RequestOTP(ctx context.Context, phone string) error {
	return s.RequestOTPWithChannel(ctx, phone, "sms")
}

func (s *Service) RequestOTPWithChannel(ctx context.Context, phone, preferredChannel string) error {
	phone = domain.NormalizePhone(phone)
	if phone == "" {
		return errors.New("phone is required")
	}
	locked, err := s.limiter.IsLocked(ctx, phone)
	if err != nil {
		return fmt.Errorf("check OTP lockout: %w", err)
	}
	if locked {
		return domain.ErrRateLimited
	}
	code, err := secureCode()
	if err != nil {
		return fmt.Errorf("generate OTP: %w", err)
	}
	if err := s.otps.Save(ctx, domain.OTP{Destination: phone, Code: code, ExpiresAt: s.clock().Add(OTPValidity)}); err != nil {
		return fmt.Errorf("save OTP: %w", err)
	}
	preferred, fallback := s.channels.SMS, s.channels.WhatsApp
	if strings.EqualFold(strings.TrimSpace(preferredChannel), "whatsapp") {
		preferred, fallback = fallback, preferred
	}
	if err := preferred.Send(ctx, phone, code); err == nil {
		return nil
	}
	if err := fallback.Send(ctx, phone, code); err != nil {
		return fmt.Errorf("SMS and WhatsApp delivery failed: %w", err)
	}
	return nil
}

func (s *Service) VerifyOTP(ctx context.Context, phone, code string) (domain.Session, error) {
	phone = domain.NormalizePhone(phone)
	locked, err := s.limiter.IsLocked(ctx, phone)
	if err != nil {
		return domain.Session{}, fmt.Errorf("check OTP lockout: %w", err)
	}
	if locked {
		return domain.Session{}, domain.ErrRateLimited
	}
	otp, err := s.otps.Find(ctx, phone)
	if err != nil || !otp.Valid(s.clock(), strings.TrimSpace(code)) {
		locked, lockErr := s.limiter.RecordFailure(ctx, phone)
		if lockErr != nil {
			return domain.Session{}, fmt.Errorf("record OTP failure: %w", lockErr)
		}
		if locked {
			return domain.Session{}, domain.ErrRateLimited
		}
		return domain.Session{}, domain.ErrInvalidOTP
	}
	if err := s.otps.Delete(ctx, phone); err != nil {
		return domain.Session{}, fmt.Errorf("consume OTP: %w", err)
	}
	if err := s.limiter.Reset(ctx, phone); err != nil {
		return domain.Session{}, fmt.Errorf("reset OTP failures: %w", err)
	}
	user, err := s.users.FindByPhone(ctx, phone)
	if err != nil {
		user, err = s.users.Create(ctx, domain.User{Phone: phone, Name: phone, Role: domain.RoleCustomer})
		if err != nil {
			return domain.Session{}, fmt.Errorf("provision user: %w", err)
		}
	}
	return s.tokens.Issue(ctx, user)
}

func (s *Service) Login(ctx context.Context, email, password string) (domain.Session, error) {
	user, err := s.users.FindByEmail(ctx, strings.TrimSpace(email))
	if err != nil || s.hasher.Compare(user.PasswordHash, password) != nil {
		return domain.Session{}, domain.ErrInvalidCredentials
	}
	return s.tokens.Issue(ctx, user)
}
func (s *Service) Refresh(ctx context.Context, refreshToken string) (domain.Session, error) {
	return s.tokens.Rotate(ctx, refreshToken)
}
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.tokens.Revoke(ctx, refreshToken)
}

func secureCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
