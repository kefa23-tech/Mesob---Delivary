package http_test

import (
	"context"
	"github.com/go-chi/chi/v5"
	authhttp "github.com/mesob-delivery/backend/internal/catalog/adapter/http"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
	"github.com/mesob-delivery/backend/internal/catalog/usecase"
	"github.com/mesob-delivery/backend/internal/platform/httpmiddleware"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type menuRepository struct {
	menu domain.Menu
	err  error
}

func (r menuRepository) GetMenu(context.Context, domain.RestaurantID) (domain.Menu, error) {
	return r.menu, r.err
}
func newMenuRouter(repository menuRepository) *chi.Mux {
	service := usecase.NewServiceWithMenu(nil, repository, nil)
	router := chi.NewRouter()
	router.Use(httpmiddleware.RequestID)
	router.Mount("/restaurants", authhttp.NewHandler(service).Routes())
	return router
}

func TestMenuEndpointReturnsNestedMenu(t *testing.T) {
	router := newMenuRouter(menuRepository{menu: domain.Menu{RestaurantID: "restaurant-1", RestaurantName: "Mesob", Categories: []domain.MenuCategory{{Name: "Mains", Items: []domain.MenuItem{{Name: "Shiro", PriceMinor: 12500, IsAvailable: true, OptionGroups: []domain.OptionGroup{{Name: "Spice", MinSelect: 1, MaxSelect: 2, Options: []domain.Option{{Name: "Mild", PriceDeltaMinor: 0, IsAvailable: true}}}}}}}}}})
	request := httptest.NewRequest(nethttp.MethodGet, "/restaurants/restaurant-1/menu", nil)
	request.Header.Set("X-Request-ID", "req-menu")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != nethttp.StatusOK || !strings.Contains(response.Body.String(), `"min_select":1`) || !strings.Contains(response.Body.String(), `"price_delta":0`) {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestMenuEndpointMapsMissingAndClosedRestaurants(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{{"missing", domain.ErrRestaurantNotFound, nethttp.StatusNotFound, "restaurant_not_found"}, {"closed", domain.ErrRestaurantClosed, nethttp.StatusConflict, "restaurant_closed"}} {
		t.Run(test.name, func(t *testing.T) {
			router := newMenuRouter(menuRepository{err: test.err})
			request := httptest.NewRequest(nethttp.MethodGet, "/restaurants/restaurant-1/menu", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || !strings.Contains(response.Body.String(), `"request_id"`) {
				t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
