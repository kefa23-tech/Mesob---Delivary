package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/auth/domain"
	"time"
)

type Clock func() time.Time
type OTPStore interface {
	Save(context.Context, domain.OTP) error
	Find(context.Context, string) (domain.OTP, error)
	Delete(context.Context, string) error
}
type UserRepository interface {
	FindByPhone(context.Context, string) (domain.User, error)
	FindByEmail(context.Context, string) (domain.User, error)
	Create(context.Context, domain.User) (domain.User, error)
}
type PasswordHasher interface {
	Hash(string) (string, error)
	Compare(hash, password string) error
}
type TokenService interface {
	Issue(context.Context, domain.User) (domain.Session, error)
	Rotate(context.Context, string) (domain.Session, error)
	Revoke(context.Context, string) error
}
type RateLimiter interface {
	IsLocked(context.Context, string) (bool, error)
	RecordFailure(context.Context, string) (bool, error)
	Reset(context.Context, string) error
}
type OTPChannel interface {
	Send(context.Context, string, string) error
}
type OTPChannels struct {
	SMS      OTPChannel
	WhatsApp OTPChannel
}
