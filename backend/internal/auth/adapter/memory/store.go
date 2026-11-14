package memory

import (
	"context"
	"errors"
	"github.com/mesob-delivery/backend/internal/auth/domain"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	mu       sync.Mutex
	OTPs     map[string]domain.OTP
	Users    map[string]domain.User
	Email    map[string]domain.User
	Failures map[string]int
	Locked   map[string]time.Time
}

func New() *Store {
	return &Store{OTPs: map[string]domain.OTP{}, Users: map[string]domain.User{}, Email: map[string]domain.User{}, Failures: map[string]int{}, Locked: map[string]time.Time{}}
}
func (s *Store) Save(_ context.Context, otp domain.OTP) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.OTPs[otp.Destination] = otp
	return nil
}
func (s *Store) Find(_ context.Context, phone string) (domain.OTP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	otp, ok := s.OTPs[phone]
	if !ok {
		return domain.OTP{}, ErrNotFound
	}
	return otp, nil
}
func (s *Store) Delete(_ context.Context, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.OTPs, phone)
	return nil
}
func (s *Store) IsLocked(_ context.Context, phone string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.Locked[phone]
	return ok && time.Now().Before(until), nil
}
func (s *Store) RecordFailure(_ context.Context, phone string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Failures[phone]++
	if s.Failures[phone] >= 5 {
		s.Locked[phone] = time.Now().Add(15 * time.Minute)
		return true, nil
	}
	return false, nil
}
func (s *Store) Reset(_ context.Context, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Failures, phone)
	delete(s.Locked, phone)
	return nil
}
func (s *Store) FindByPhone(_ context.Context, phone string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.Users[phone]
	if !ok {
		return domain.User{}, ErrNotFound
	}
	return user, nil
}
func (s *Store) FindByEmail(_ context.Context, email string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.Email[email]
	if !ok {
		return domain.User{}, ErrNotFound
	}
	return user, nil
}
func (s *Store) Create(_ context.Context, user domain.User) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user.ID = domain.UserID(time.Now().UTC().Format("20060102150405.000000000"))
	s.Users[user.Phone] = user
	if user.Email != "" {
		s.Email[user.Email] = user
	}
	return user, nil
}
