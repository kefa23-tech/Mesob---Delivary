package http

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/orders/usecase"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	service      *usecase.GroupCartService
	shareBaseURL string
}

func NewHandler(service *usecase.GroupCartService, shareBaseURL string) *Handler {
	return &Handler{service: service, shareBaseURL: strings.TrimRight(shareBaseURL, "/")}
}
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/{token}", h.get)
	r.Post("/{token}/items", h.addItem)
	r.Delete("/{token}/items/{itemID}", h.removeItem)
	r.Post("/{token}/lock", h.lock)
	return r
}

type createRequest struct {
	RestaurantID string `json:"restaurant_id"`
	DisplayName  string `json:"display_name"`
	UserID       string `json:"user_id,omitempty"`
}
type itemRequest struct {
	MenuItemID     string                  `json:"menu_item_id"`
	RestaurantID   string                  `json:"restaurant_id"`
	Name           string                  `json:"name,omitempty"`
	UnitPriceMinor int64                   `json:"unit_price_minor,omitempty"`
	Quantity       int                     `json:"quantity"`
	Options        []domain.SelectedOption `json:"options"`
}
type cartResponse struct {
	Cart     domain.GroupCart `json:"cart"`
	ShareURL string           `json:"share_url"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in createRequest
	if !decode(w, r, &in) {
		return
	}
	cart, err := h.service.Create(r.Context(), in.RestaurantID, participant(r, in.UserID, in.DisplayName))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, cartResponse{Cart: cart, ShareURL: h.shareURL(r, cart.Token)})
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	cart, err := h.service.Get(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cartResponse{Cart: cart, ShareURL: h.shareURL(r, cart.Token)})
}
func (h *Handler) addItem(w http.ResponseWriter, r *http.Request) {
	var in itemRequest
	if !decode(w, r, &in) {
		return
	}
	cart, err := h.service.AddItem(r.Context(), chi.URLParam(r, "token"), domain.CartItem{ID: newItemID(), MenuItemID: in.MenuItemID, RestaurantID: in.RestaurantID, Name: in.Name, UnitPriceMinor: in.UnitPriceMinor, Quantity: in.Quantity, Options: in.Options, Participant: participant(r, r.Header.Get("X-User-ID"), r.Header.Get("X-Guest-Name"))})
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, cartResponse{Cart: cart, ShareURL: h.shareURL(r, cart.Token)})
}
func (h *Handler) removeItem(w http.ResponseWriter, r *http.Request) {
	cart, err := h.service.RemoveItem(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "itemID"))
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cartResponse{Cart: cart, ShareURL: h.shareURL(r, cart.Token)})
}
func (h *Handler) lock(w http.ResponseWriter, r *http.Request) {
	cart, err := h.service.Lock(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cartResponse{Cart: cart, ShareURL: h.shareURL(r, cart.Token)})
}
func participant(r *http.Request, userID, displayName string) domain.Participant {
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}
	if displayName == "" {
		displayName = r.Header.Get("X-Guest-Name")
	}
	if displayName == "" {
		displayName = "Guest"
	}
	return domain.Participant{UserID: userID, DisplayName: displayName}
}
func newItemID() string {
	return strings.ReplaceAll(strings.TrimSpace(timeNow().Format("20060102150405.000000000")), ".", "-")
}

var timeNow = func() time.Time { return time.Now() }

func (h *Handler) shareURL(r *http.Request, token string) string {
	if h.shareBaseURL != "" {
		return h.shareBaseURL + "/group-cart/" + token
	}
	return "https://" + r.Host + "/group-cart/" + token
}
func status(err error) int {
	switch {
	case errors.Is(err, domain.ErrCartNotFound), errors.Is(err, domain.ErrItemNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrCartLocked):
		return http.StatusConflict
	case errors.Is(err, domain.ErrRestaurantMismatch), errors.Is(err, domain.ErrInvalidQuantity), errors.Is(err, domain.ErrInvalidSelection):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": "group_cart_error", "message": err.Error()}})
}
