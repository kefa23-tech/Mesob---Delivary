package domain_test

import (
	"errors"
	"testing"

	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

func TestValidateCODAssignment(t *testing.T) {
	if err := domain.ValidateCODAssignment(200_000, 100_000, 200_000, 500_000); err != nil {
		t.Fatalf("assignment exactly at limit rejected: %v", err)
	}
	if err := domain.ValidateCODAssignment(300_001, 0, 1, 300_001); !errors.Is(err, domain.ErrCourierCODLimit) {
		t.Fatalf("expected limit rejection, got %v", err)
	}
}
