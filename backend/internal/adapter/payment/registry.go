package payment

import "github.com/mesob-delivery/backend/internal/payment/domain"

type Registry map[domain.Provider]domain.Gateway

func (r Registry) Gateway(provider domain.Provider) (domain.Gateway, bool) {
	gateway, ok := r[provider]
	return gateway, ok
}
