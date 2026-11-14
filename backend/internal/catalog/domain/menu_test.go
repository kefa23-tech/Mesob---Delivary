package domain_test

import (
	"errors"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
	"testing"
)

func testGroup() domain.OptionGroup {
	return domain.OptionGroup{MinSelect: 1, MaxSelect: 2, Options: []domain.Option{{ID: "mild", Name: "Mild", IsAvailable: true}, {ID: "extra", Name: "Extra", IsAvailable: true}, {ID: "sold-out", Name: "Sold out", IsAvailable: false}}}
}

func TestValidateOptionSelectionMinMax(t *testing.T) {
	group := testGroup()
	if err := domain.ValidateOptionSelection(group, nil); !errors.Is(err, domain.ErrTooFewOptions) {
		t.Fatalf("expected min constraint, got %v", err)
	}
	if err := domain.ValidateOptionSelection(group, []string{"mild", "extra", "sold-out"}); !errors.Is(err, domain.ErrTooManyOptions) {
		t.Fatalf("expected max constraint, got %v", err)
	}
	if err := domain.ValidateOptionSelection(group, []string{"mild"}); err != nil {
		t.Fatalf("valid selection rejected: %v", err)
	}
}

func TestValidateOptionSelectionRejectsInvalidOptions(t *testing.T) {
	group := testGroup()
	if err := domain.ValidateOptionSelection(group, []string{"mild", "mild"}); !errors.Is(err, domain.ErrDuplicateOption) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if err := domain.ValidateOptionSelection(group, []string{"sold-out"}); !errors.Is(err, domain.ErrUnavailableOption) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
	if err := domain.ValidateOptionSelection(group, []string{"unknown"}); !errors.Is(err, domain.ErrUnknownOption) {
		t.Fatalf("expected unknown error, got %v", err)
	}
}
