package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mesob-delivery/backend/internal/auth/domain"
	"sync"
	"time"
)

var ErrInvalidRefresh = errors.New("invalid refresh token")

type Store interface {
	SaveRefresh(context.Context, string, domain.User, time.Duration) error
	Take(context.Context, string) (domain.User, error)
	DeleteRefresh(context.Context, string) error
}
type Service struct {
	secret                []byte
	accessTTL, refreshTTL time.Duration
	store                 Store
	mu                    sync.Mutex
	local                 map[string]domain.User
}

func New(secret string) *Service {
	return &Service{secret: []byte(secret), accessTTL: 15 * time.Minute, refreshTTL: 30 * 24 * time.Hour, local: map[string]domain.User{}}
}
func NewWithStore(secret string, store Store) *Service {
	service := New(secret)
	service.store = store
	return service
}
func (s *Service) Issue(ctx context.Context, user domain.User) (domain.Session, error) {
	now := time.Now()
	claims := jwt.MapClaims{"sub": string(user.ID), "role": string(user.Role), "iat": now.Unix(), "exp": now.Add(s.accessTTL).Unix()}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return domain.Session{}, err
	}
	refresh, err := randomToken()
	if err != nil {
		return domain.Session{}, err
	}
	key := hash(refresh)
	if s.store != nil {
		if err := s.store.SaveRefresh(ctx, key, user, s.refreshTTL); err != nil {
			return domain.Session{}, err
		}
	} else {
		s.mu.Lock()
		s.local[key] = user
		s.mu.Unlock()
	}
	return domain.Session{AccessToken: signed, RefreshToken: refresh, ExpiresIn: int64(s.accessTTL.Seconds()), User: user}, nil
}
func (s *Service) Rotate(ctx context.Context, old string) (domain.Session, error) {
	key := hash(old)
	var user domain.User
	var err error
	if s.store != nil {
		user, err = s.store.Take(ctx, key)
	} else {
		s.mu.Lock()
		var ok bool
		user, ok = s.local[key]
		delete(s.local, key)
		s.mu.Unlock()
		if !ok {
			err = ErrInvalidRefresh
		}
	}
	if err != nil {
		return domain.Session{}, ErrInvalidRefresh
	}
	return s.Issue(ctx, user)
}
func (s *Service) Revoke(ctx context.Context, refresh string) error {
	key := hash(refresh)
	if s.store != nil {
		return s.store.DeleteRefresh(ctx, key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.local, key)
	return nil
}
func hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
