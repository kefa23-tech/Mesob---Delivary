package usecase

import (
	"context"
	"strings"

	"github.com/mesob-delivery/backend/internal/admin/domain"
)

type OperationsRepository interface {
	Overview(context.Context) (domain.Overview, error)
	PendingKYC(context.Context) (domain.KYCQueue, error)
	ReviewKYC(context.Context, domain.ReviewRequest, string) error
	StuckOrders(context.Context, int) ([]domain.StuckOrder, error)
	ManualAssign(context.Context, domain.AssignmentRequest, string) error
	ResolveDispute(context.Context, domain.DisputeRequest, string) error
	LedgerSummary(context.Context) (domain.LedgerSummary, error)
	LedgerEntries(context.Context, int, int, string, string) (domain.LedgerPage, error)
	SettleCOD(context.Context, domain.SettlementRequest, string) error
}

type Operations struct{ repository OperationsRepository }

func NewOperations(repository OperationsRepository) *Operations {
	return &Operations{repository: repository}
}
func (o *Operations) Overview(ctx context.Context) (domain.Overview, error) {
	return o.repository.Overview(ctx)
}
func (o *Operations) PendingKYC(ctx context.Context) (domain.KYCQueue, error) {
	return o.repository.PendingKYC(ctx)
}
func (o *Operations) ReviewKYC(ctx context.Context, req domain.ReviewRequest, adminID string) error {
	if req.ID == "" || !domain.ValidDecision(req.Decision) || !domain.ValidNotes(req.Notes) || (req.EntityType != "restaurant" && req.EntityType != "courier") {
		return domain.ErrInvalidRequest
	}
	req.Notes = strings.TrimSpace(req.Notes)
	return o.repository.ReviewKYC(ctx, req, adminID)
}
func (o *Operations) StuckOrders(ctx context.Context, minutes int) ([]domain.StuckOrder, error) {
	if minutes < 1 || minutes > 1440 {
		minutes = 20
	}
	return o.repository.StuckOrders(ctx, minutes)
}
func (o *Operations) ManualAssign(ctx context.Context, req domain.AssignmentRequest, adminID string) error {
	if req.OrderID == "" || req.CourierID == "" {
		return domain.ErrInvalidRequest
	}
	req.AdminID = adminID
	return o.repository.ManualAssign(ctx, req, adminID)
}
func (o *Operations) ResolveDispute(ctx context.Context, req domain.DisputeRequest, adminID string) error {
	if req.OrderID == "" || !domain.ValidResolution(req.Resolution) || !domain.ValidNotes(req.Notes) || req.RefundMinor < 0 {
		return domain.ErrInvalidRequest
	}
	req.AdminID = adminID
	req.Notes = strings.TrimSpace(req.Notes)
	return o.repository.ResolveDispute(ctx, req, adminID)
}
func (o *Operations) LedgerSummary(ctx context.Context) (domain.LedgerSummary, error) {
	return o.repository.LedgerSummary(ctx)
}
func (o *Operations) LedgerEntries(ctx context.Context, page, limit int, accountType, query string) (domain.LedgerPage, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	return o.repository.LedgerEntries(ctx, page, limit, accountType, strings.TrimSpace(query))
}
func (o *Operations) SettleCOD(ctx context.Context, req domain.SettlementRequest, adminID string) error {
	if req.CourierID == "" || req.Reference == "" || req.AmountMinor <= 0 {
		return domain.ErrInvalidRequest
	}
	req.AdminID = adminID
	return o.repository.SettleCOD(ctx, req, adminID)
}
