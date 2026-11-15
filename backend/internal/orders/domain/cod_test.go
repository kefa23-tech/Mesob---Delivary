package domain_test

import (
	"errors"
	"testing"

	"github.com/mesob-delivery/backend/internal/orders/domain"
)

func TestValidateCODEligibility(t *testing.T) {
	tests := []struct {
		name          string
		total         int64
		completed     int64
		cancellations int64
		suspended     bool
		want          error
	}{
		{name: "eligible at cap", total: domain.MaxCODOrderMinor, completed: 1},
		{name: "over cap", total: domain.MaxCODOrderMinor + 1, completed: 1, want: domain.ErrCODOrderLimit},
		{name: "new account", total: 100, want: domain.ErrCODFirstOrder},
		{name: "unpaid cancellation", total: 100, completed: 1, cancellations: 1, want: domain.ErrCODUnpaidCancellation},
		{name: "suspended", total: 100, completed: 1, suspended: true, want: domain.ErrCODSuspended},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateCODEligibility(tt.total, tt.completed, tt.cancellations, tt.suspended)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateCODEligibility() error = %v, want %v", err, tt.want)
			}
		})
	}
}
