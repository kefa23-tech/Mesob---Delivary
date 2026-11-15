package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/admin/domain"
	"github.com/mesob-delivery/backend/internal/admin/usecase"
)

type Operations interface {
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
type Handler struct {
	operations Operations
	admins     AdminChecker
	dlq        usecase.DLQInspector
}

type AdminChecker interface {
	IsActiveAdmin(context.Context, string) (bool, error)
}
type postgresAdminChecker struct{ database *pgxpool.Pool }

func (c postgresAdminChecker) IsActiveAdmin(ctx context.Context, userID string) (bool, error) {
	var role, status string
	err := c.database.QueryRow(ctx, `SELECT role,status FROM users WHERE id=$1`, userID).Scan(&role, &status)
	return err == nil && role == "admin" && status == "active", err
}

func NewHandler(operations *usecase.Operations, database *pgxpool.Pool) *Handler {
	return &Handler{operations: operations, admins: postgresAdminChecker{database: database}}
}
func NewHandlerWithAdminChecker(operations Operations, admins AdminChecker) *Handler {
	return &Handler{operations: operations, admins: admins}
}
func (h *Handler) WithDLQ(dlq usecase.DLQInspector) *Handler { h.dlq = dlq; return h }
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(h.adminRequired)
	r.Get("/overview", h.overview)
	r.Get("/kyc/pending", h.pendingKYC)
	r.Post("/kyc/{entity_type}/{id}/review", h.reviewKYC)
	r.Get("/orders/stuck", h.stuckOrders)
	r.Post("/orders/{id}/manual-assign", h.manualAssign)
	r.Post("/orders/{id}/resolve-dispute", h.resolveDispute)
	r.Get("/ledger/summary", h.ledgerSummary)
	r.Get("/ledger/entries", h.ledgerEntries)
	r.Post("/couriers/{id}/settle-cod", h.settleCOD)
	r.Get("/ops/dlq", h.listDLQ)
	r.Post("/ops/dlq/{queue}/{task_id}/retry", h.retryDLQ)
	r.Delete("/ops/dlq/{queue}/{task_id}", h.deleteDLQ)
	return r
}
func (h *Handler) listDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlq == nil {
		writeError(w, errors.New("DLQ inspector unavailable"))
		return
	}
	tasks, depths, err := h.dlq.List(r.Context())
	write(w, map[string]any{"tasks": tasks, "queue_depths": depths}, err)
}
func (h *Handler) retryDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlq == nil {
		writeError(w, errors.New("DLQ inspector unavailable"))
		return
	}
	queue, id := chi.URLParam(r, "queue"), chi.URLParam(r, "task_id")
	if err := h.dlq.Retry(r.Context(), queue, id); err != nil {
		write(w, nil, err)
		return
	}
	slog.WarnContext(r.Context(), "admin replayed dead-letter task", "admin_id", r.Header.Get("X-User-ID"), "queue", queue, "task_id", id)
	write(w, map[string]bool{"ok": true}, nil)
}
func (h *Handler) deleteDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlq == nil {
		writeError(w, errors.New("DLQ inspector unavailable"))
		return
	}
	queue, id := chi.URLParam(r, "queue"), chi.URLParam(r, "task_id")
	if err := h.dlq.Delete(r.Context(), queue, id); err != nil {
		write(w, nil, err)
		return
	}
	slog.WarnContext(r.Context(), "admin acknowledged dead-letter task", "admin_id", r.Header.Get("X-User-ID"), "queue", queue, "task_id", id)
	write(w, map[string]bool{"ok": true}, nil)
}
func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	out, err := h.operations.Overview(r.Context())
	write(w, out, err)
}
func (h *Handler) adminRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
		if userID == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		allowed, err := h.admins.IsActiveAdmin(r.Context(), userID)
		if err != nil || !allowed {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) pendingKYC(w http.ResponseWriter, r *http.Request) {
	out, err := h.operations.PendingKYC(r.Context())
	write(w, out, err)
}
func (h *Handler) reviewKYC(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
		Notes    string `json:"notes"`
	}
	if decode(w, r, &in) != nil {
		writeError(w, domain.ErrInvalidRequest)
		return
	}
	err := h.operations.ReviewKYC(r.Context(), domain.ReviewRequest{EntityType: chi.URLParam(r, "entity_type"), ID: chi.URLParam(r, "id"), Decision: strings.ToLower(in.Decision), Notes: in.Notes}, r.Header.Get("X-User-ID"))
	write(w, map[string]bool{"ok": err == nil}, err)
}
func (h *Handler) stuckOrders(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("sla_minutes"))
	out, err := h.operations.StuckOrders(r.Context(), n)
	write(w, map[string]any{"data": out}, err)
}
func (h *Handler) manualAssign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CourierID string `json:"courier_id"`
	}
	if decode(w, r, &in) != nil {
		writeError(w, domain.ErrInvalidRequest)
		return
	}
	err := h.operations.ManualAssign(r.Context(), domain.AssignmentRequest{OrderID: chi.URLParam(r, "id"), CourierID: in.CourierID}, r.Header.Get("X-User-ID"))
	write(w, map[string]bool{"ok": err == nil}, err)
}
func (h *Handler) resolveDispute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Resolution  string `json:"resolution"`
		RefundMinor int64  `json:"refund_minor"`
		Notes       string `json:"notes"`
	}
	if decode(w, r, &in) != nil {
		writeError(w, domain.ErrInvalidRequest)
		return
	}
	err := h.operations.ResolveDispute(r.Context(), domain.DisputeRequest{OrderID: chi.URLParam(r, "id"), Resolution: strings.ToLower(in.Resolution), RefundMinor: in.RefundMinor, Notes: in.Notes}, r.Header.Get("X-User-ID"))
	write(w, map[string]bool{"ok": err == nil}, err)
}
func (h *Handler) ledgerSummary(w http.ResponseWriter, r *http.Request) {
	out, err := h.operations.LedgerSummary(r.Context())
	write(w, out, err)
}
func (h *Handler) ledgerEntries(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.operations.LedgerEntries(r.Context(), page, limit, r.URL.Query().Get("account_type"), r.URL.Query().Get("q"))
	write(w, out, err)
}
func (h *Handler) settleCOD(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reference   string `json:"reference"`
		AmountMinor int64  `json:"amount_minor"`
	}
	if decode(w, r, &in) != nil {
		writeError(w, domain.ErrInvalidRequest)
		return
	}
	err := h.operations.SettleCOD(r.Context(), domain.SettlementRequest{CourierID: chi.URLParam(r, "id"), Reference: in.Reference, AmountMinor: in.AmountMinor}, r.Header.Get("X-User-ID"))
	write(w, map[string]bool{"ok": err == nil}, err)
}

func decode(w http.ResponseWriter, r *http.Request, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func write(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	if errors.Is(err, domain.ErrInvalidRequest) {
		status = http.StatusBadRequest
		code = "invalid_request"
	} else if errors.Is(err, domain.ErrNotFound) {
		status = http.StatusNotFound
		code = "not_found"
	} else if errors.Is(err, domain.ErrConflict) {
		status = http.StatusConflict
		code = "conflict"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": err.Error()})
}
