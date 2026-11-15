package domain

import "errors"

var (
	ErrCourierCODLimit    = errors.New("courier COD cash limit would be exceeded")
	ErrCourierIneligible  = errors.New("courier is not active and eligible")
	ErrOrderNotAssignable = errors.New("order not found or not assignable")
)

const DefaultCourierCODLimitMinor int64 = 500_000

func ValidateCODAssignment(balanceMinor, reservedMinor, orderMinor, limitMinor int64) error {
	if balanceMinor < 0 || reservedMinor < 0 || orderMinor < 0 || limitMinor < 0 || balanceMinor+reservedMinor+orderMinor > limitMinor {
		return ErrCourierCODLimit
	}
	return nil
}
