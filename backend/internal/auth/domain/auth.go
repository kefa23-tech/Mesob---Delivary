package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidOTP         = errors.New("invalid or expired OTP")
	ErrRateLimited        = errors.New("too many failed verification attempts")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type Role string

const (
	RoleCustomer Role = "customer"
	RoleCourier  Role = "courier"
	RoleAdmin    Role = "admin"
)

type UserID string

type User struct {
	ID           UserID `json:"id"`
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Email        string `json:"email,omitempty"`
	PasswordHash string `json:"-"`
	Role         Role   `json:"role"`
}

type OTP struct {
	Destination string
	Code        string
	ExpiresAt   time.Time
}

func (o OTP) Valid(now time.Time, code string) bool { return o.Code == code && now.Before(o.ExpiresAt) }

type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	User         User   `json:"user"`
}

func NormalizePhone(phone string) string { return strings.TrimSpace(phone) }
