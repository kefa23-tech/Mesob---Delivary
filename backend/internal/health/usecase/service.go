package usecase

import (
	"context"
	"time"

	"github.com/mesob-delivery/backend/internal/health/domain"
)

type Clock func() time.Time

type HealthChecker interface {
	Check(context.Context) domain.Report
}

type Service struct {
	now Clock
}

func NewService(now Clock) *Service { return &Service{now: now} }

func (s *Service) Check(context.Context) domain.Report {
	return domain.Report{Status: domain.StatusOK, Service: "mesob-api", Timestamp: s.now().UTC()}
}
