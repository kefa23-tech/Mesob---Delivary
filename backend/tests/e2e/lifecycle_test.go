//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pgadapter "github.com/mesob-delivery/backend/internal/adapter/postgres"
	cataloghttp "github.com/mesob-delivery/backend/internal/catalog/adapter/http"
	catalogpostgres "github.com/mesob-delivery/backend/internal/catalog/adapter/postgres"
	catalogusecase "github.com/mesob-delivery/backend/internal/catalog/usecase"
	dispatchhttp "github.com/mesob-delivery/backend/internal/dispatch/adapter/http"
	dispatchpostgres "github.com/mesob-delivery/backend/internal/dispatch/adapter/postgres"
	dispatchredis "github.com/mesob-delivery/backend/internal/dispatch/adapter/redis"
	dispatchdomain "github.com/mesob-delivery/backend/internal/dispatch/domain"
	dispatchusecase "github.com/mesob-delivery/backend/internal/dispatch/usecase"
	ledgerpostgres "github.com/mesob-delivery/backend/internal/ledger/adapter/postgres"
	ledgerusecase "github.com/mesob-delivery/backend/internal/ledger/usecase"
	ordershttp "github.com/mesob-delivery/backend/internal/orders/adapter/http"
	orderspostgres "github.com/mesob-delivery/backend/internal/orders/adapter/postgres"
	ordersredis "github.com/mesob-delivery/backend/internal/orders/adapter/redis"
	ordersdomain "github.com/mesob-delivery/backend/internal/orders/domain"
	ordersusecase "github.com/mesob-delivery/backend/internal/orders/usecase"
	paymenthttp "github.com/mesob-delivery/backend/internal/payment/adapter/http"
	paymentpostgres "github.com/mesob-delivery/backend/internal/payment/adapter/postgres"
	paymentdomain "github.com/mesob-delivery/backend/internal/payment/domain"
	paymentusecase "github.com/mesob-delivery/backend/internal/payment/usecase"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const webhookSecret = "e2e-chapa-webhook-secret"

func TestMesobFullLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	databaseURL := startPostgres(t, ctx)
	redisURL := startRedis(t, ctx)
	migrations, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateReady(ctx, databaseURL, migrations); err != nil {
		t.Fatal(err)
	}
	pool, err := pgadapter.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var ownerID, customerID, restaurantID, addressID, categoryID, itemID, courierA, courierB string
	for _, user := range []struct {
		name, phone, role string
		into              *string
	}{
		{"E2E Restaurant Owner", "+251900000801", "restaurant_owner", &ownerID},
		{"E2E Customer", "+251900000802", "customer", &customerID},
		{"Courier One", "+251900000803", "courier", &courierA},
		{"Courier Two", "+251900000804", "courier", &courierB},
	} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(name,phone,role,status) VALUES($1,$2,$3,'active') RETURNING id::text`, user.name, user.phone, user.role).Scan(user.into); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'Sofur Lifecycle Kitchen',9,38.75,5,true,10,'approved') RETURNING id::text`, ownerID).Scan(&restaurantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO restaurants(owner_id,name,lat,lng,delivery_radius_km,is_open,commission_pct,approval_status) VALUES($1,'Nearby Search Kitchen',9.001,38.75,3,true,10,'approved'),($1,'Outside Search Kitchen',9.1,38.75,2,true,10,'approved')`, ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO addresses(user_id,label,lat,lng,line1) VALUES($1,'E2E Home',9,38.75,'Lifecycle Test Street') RETURNING id::text`, customerID).Scan(&addressID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_categories(restaurant_id,name) VALUES($1,'Group Meals') RETURNING id::text`, restaurantID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items(category_id,name,price_minor,stock_limit,stock_remaining,is_available) VALUES($1,'Sofur Platter',60000,20,20,true) RETURNING id::text`, categoryID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	catalogRepository := catalogpostgres.NewRepository(pool)
	catalogRouter := cataloghttp.NewHandler(catalogusecase.NewServiceWithMenu(catalogRepository, catalogRepository, nil)).Routes()
	search := httptest.NewRecorder()
	catalogRouter.ServeHTTP(search, httptest.NewRequest(http.MethodGet, "/?lat=9&lng=38.75&limit=20", nil))
	if search.Code != http.StatusOK {
		t.Fatalf("restaurant search status=%d body=%s", search.Code, search.Body.String())
	}
	var searchResult struct {
		Restaurants []struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			Distance float64 `json:"distance_meters"`
		} `json:"restaurants"`
	}
	if err := json.Unmarshal(search.Body.Bytes(), &searchResult); err != nil {
		t.Fatal(err)
	}
	if len(searchResult.Restaurants) != 2 || searchResult.Restaurants[0].Name != "Sofur Lifecycle Kitchen" || searchResult.Restaurants[1].Distance < searchResult.Restaurants[0].Distance {
		t.Fatalf("ST_DWithin filter/order failed: %+v", searchResult.Restaurants)
	}

	orderRepository := orderspostgres.NewRepository(pool)
	cartStore, err := ordersredis.New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer cartStore.Close()
	cartService := ordersusecase.NewGroupCartService(cartStore, orderRepository, 0)
	cartRouter := ordershttp.NewHandler(cartService, "").Routes()
	createBody, _ := json.Marshal(map[string]string{"restaurant_id": restaurantID, "display_name": "Lifecycle Host"})
	created := request(t, cartRouter, http.MethodPost, "/", createBody, map[string]string{"X-User-ID": customerID})
	if created.Code != http.StatusCreated {
		t.Fatalf("group cart create status=%d body=%s", created.Code, created.Body.String())
	}
	var cartEnvelope struct {
		Cart ordersdomain.GroupCart `json:"cart"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &cartEnvelope); err != nil {
		t.Fatal(err)
	}
	for _, participant := range []struct{ id, name string }{{customerID, "Lifecycle Host"}, {"guest-sofur-2", "Sofur Guest"}} {
		body, _ := json.Marshal(map[string]any{"restaurant_id": restaurantID, "menu_item_id": itemID, "quantity": 1})
		added := request(t, cartRouter, http.MethodPost, "/"+cartEnvelope.Cart.Token+"/items", body, map[string]string{"X-User-ID": participant.id, "X-Guest-Name": participant.name})
		if added.Code != http.StatusCreated {
			t.Fatalf("participant add status=%d body=%s", added.Code, added.Body.String())
		}
	}
	cart, err := cartService.Get(ctx, cartEnvelope.Cart.Token)
	if err != nil {
		t.Fatal(err)
	}
	if cart.SubtotalMinor != 120000 || cart.DeliveryDiscountMinor != 7500 || cart.DeliveryFeeMinor != 7500 || len(cart.Participants) != 2 {
		t.Fatalf("unexpected Sofur totals/participants: %+v", cart)
	}
	locked := request(t, cartRouter, http.MethodPost, "/"+cart.Token+"/lock", nil, nil)
	if locked.Code != http.StatusOK {
		t.Fatalf("cart lock status=%d body=%s", locked.Code, locked.Body.String())
	}

	pgLedger := ledgerpostgres.NewRepository(pool)
	ledgerService := ledgerusecase.NewService(pgLedger)
	paymentQueue := &testPaymentQueue{}
	checkout := ordersusecase.NewCheckoutService(orderRepository, cartStore, cartStore, paymentQueue)
	checkoutRouter := ordershttp.NewCheckoutHandler(checkout).Routes()
	checkoutBody, _ := json.Marshal(ordersdomain.CheckoutRequest{AddressID: addressID, PaymentMethod: ordersdomain.PaymentChapa, GroupCartToken: cart.Token})
	checkoutHeaders := map[string]string{"X-User-ID": customerID, "Idempotency-Key": "e2e-sofur-checkout"}
	first := request(t, checkoutRouter, http.MethodPost, "/", checkoutBody, checkoutHeaders)
	if first.Code != http.StatusCreated {
		t.Fatalf("checkout status=%d body=%s", first.Code, first.Body.String())
	}
	var order ordersdomain.Order
	if err := json.Unmarshal(first.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	if order.SubtotalMinor != 120000 || order.DeliveryFeeMinor != 7500 || order.TotalMinor != 127500 || order.DeliveryPIN == "" {
		t.Fatalf("checkout lost group discount or PIN: %+v", order)
	}
	replay := request(t, checkoutRouter, http.MethodPost, "/", checkoutBody, checkoutHeaders)
	if replay.Code != http.StatusCreated || !bytes.Equal(first.Body.Bytes(), replay.Body.Bytes()) {
		t.Fatalf("idempotent replay differs: first=%d replay=%d", first.Code, replay.Code)
	}

	verificationGateway := &fakeGateway{amount: order.TotalMinor}
	payments := paymentusecase.NewService(paymentpostgres.NewRepository(pool, ledgerService), fakeGateways{paymentdomain.Chapa: verificationGateway}, paymentQueue, "https://api.e2e.invalid", "https://app.e2e.invalid")
	if _, err := payments.Initialize(ctx, customerID, order.ID, ""); err != nil {
		t.Fatal(err)
	}
	webhookBody := []byte(fmt.Sprintf(`{"tx_ref":%q,"status":"success"}`, order.ID))
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	_, _ = mac.Write(webhookBody)
	webhookHandler := paymenthttp.NewHandler(payments, webhookSecret, "").Routes()
	webhook := httptest.NewRequest(http.MethodPost, "/webhook/chapa", bytes.NewReader(webhookBody))
	webhook.Header.Set("X-Chapa-Signature", hex.EncodeToString(mac.Sum(nil)))
	webhookResponse := httptest.NewRecorder()
	webhookHandler.ServeHTTP(webhookResponse, webhook)
	if webhookResponse.Code != http.StatusOK {
		t.Fatalf("signed webhook status=%d body=%s", webhookResponse.Code, webhookResponse.Body.String())
	}

	presence, err := dispatchredis.New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	dispatchTasks := &testDispatchQueue{}
	dispatchService := dispatchusecase.NewService(dispatchpostgres.NewRepository(pool), presence, dispatchTasks, presence)
	kitchenService := ordersusecase.NewKitchenService(orderRepository, paymentQueue, dispatchService)
	kitchenRouter := ordershttp.NewKitchenHandler(kitchenService).Routes()
	ownerHeaders := map[string]string{"X-User-ID": ownerID}
	accepted := request(t, kitchenRouter, http.MethodPost, "/"+order.ID+"/accept", []byte(`{"prep_minutes":20}`), ownerHeaders)
	if accepted.Code != http.StatusOK {
		t.Fatalf("restaurant accept status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	preparing := request(t, kitchenRouter, http.MethodPost, "/"+order.ID+"/preparing", nil, ownerHeaders)
	if preparing.Code != http.StatusOK {
		t.Fatalf("restaurant preparing status=%d body=%s", preparing.Code, preparing.Body.String())
	}
	ready := request(t, kitchenRouter, http.MethodPost, "/"+order.ID+"/ready", nil, ownerHeaders)
	if ready.Code != http.StatusOK {
		t.Fatalf("restaurant ready status=%d body=%s", ready.Code, ready.Body.String())
	}
	for _, courier := range []string{courierA, courierB} {
		if err := dispatchService.SetStatus(ctx, dispatchdomain.CourierID(courier), true, dispatchdomain.VehicleMotorbike); err != nil {
			t.Fatal(err)
		}
		if err := dispatchService.SetLocation(ctx, dispatchdomain.CourierID(courier), 9.0001, 38.75); err != nil {
			t.Fatal(err)
		}
	}
	nearby, err := presence.Nearby(ctx, 9, 38.75, 1.5, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(nearby) != 2 {
		t.Fatalf("Redis GEOSEARCH courier matches=%d, want 2", len(nearby))
	}
	if err := dispatchService.FindCourier(ctx, dispatchdomain.OrderID(order.ID), dispatchdomain.TierOne); err != nil {
		t.Fatal(err)
	}
	dispatchRouter := dispatchhttp.NewHandler(dispatchService).Routes()
	start := make(chan struct{})
	type claimResult struct {
		courier string
		status  int
	}
	claims := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, courier := range []string{courierA, courierB} {
		wg.Add(1)
		go func(courier string) {
			defer wg.Done()
			<-start
			response := request(t, dispatchRouter, http.MethodPost, "/offers/"+order.ID+"/accept", nil, map[string]string{"X-User-ID": courier})
			claims <- claimResult{courier, response.Code}
		}(courier)
	}
	close(start)
	wg.Wait()
	close(claims)
	winners := 0
	winner := ""
	for result := range claims {
		if result.status == http.StatusOK {
			winners++
			winner = result.courier
		} else if result.status != http.StatusConflict && result.status != http.StatusGone {
			t.Fatalf("losing courier claim status=%d", result.status)
		}
	}
	if winners != 1 {
		t.Fatalf("SET NX courier claims succeeded=%d, want exactly one", winners)
	}
	pickup := request(t, dispatchRouter, http.MethodPost, "/deliveries/"+order.ID+"/pickup", nil, map[string]string{"X-User-ID": winner})
	if pickup.Code != http.StatusOK {
		t.Fatalf("pickup status=%d body=%s", pickup.Code, pickup.Body.String())
	}
	completeBody, _ := json.Marshal(map[string]string{"pin": order.DeliveryPIN})
	completed := request(t, dispatchRouter, http.MethodPost, "/deliveries/"+order.ID+"/complete", completeBody, map[string]string{"X-User-ID": winner})
	if completed.Code != http.StatusOK {
		t.Fatalf("delivery completion status=%d body=%s", completed.Code, completed.Body.String())
	}
	var finalStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, order.ID).Scan(&finalStatus); err != nil {
		t.Fatal(err)
	}
	if finalStatus != "DELIVERED" {
		t.Fatalf("final order status=%s", finalStatus)
	}
	var signedTotal int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(ll.amount_minor),0) FROM ledger_lines ll`).Scan(&signedTotal); err != nil {
		t.Fatal(err)
	}
	if signedTotal != 0 {
		t.Fatalf("global double-entry invariant sum=%d", signedTotal)
	}
	var relevantAccounts int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT la.account_type) FROM ledger_lines ll JOIN ledger_accounts la ON la.id=ll.account_id WHERE la.account_type IN ('platform_revenue','restaurant_payable','courier_payable')`).Scan(&relevantAccounts); err != nil {
		t.Fatal(err)
	}
	if relevantAccounts != 3 {
		t.Fatalf("ledger account types present=%d, want platform/restaurant/courier payable", relevantAccounts)
	}
}

type fakeGateway struct{ amount int64 }

func (g *fakeGateway) Initialize(_ context.Context, in paymentdomain.InitializeRequest) (paymentdomain.InitializeResult, error) {
	return paymentdomain.InitializeResult{Reference: in.Reference, CheckoutURL: "https://checkout.e2e.invalid"}, nil
}
func (g *fakeGateway) Verify(_ context.Context, reference string) (paymentdomain.Verification, error) {
	return paymentdomain.Verification{Reference: reference, Status: paymentdomain.Paid, AmountMinor: g.amount, Currency: "ETB"}, nil
}

type fakeGateways map[paymentdomain.Provider]paymentdomain.Gateway

func (g fakeGateways) Gateway(p paymentdomain.Provider) (paymentdomain.Gateway, bool) {
	v, ok := g[p]
	return v, ok
}

type testPaymentQueue struct{}

func (*testPaymentQueue) SchedulePoll(context.Context, paymentdomain.Provider, string, int, time.Duration) error {
	return nil
}
func (*testPaymentQueue) CancelPolling(context.Context, paymentdomain.Provider, string) error {
	return nil
}
func (*testPaymentQueue) NotifyRestaurantPaid(context.Context, string) error { return nil }
func (*testPaymentQueue) ScheduleRestaurantSLA(context.Context, string, time.Duration) error {
	return nil
}
func (*testPaymentQueue) CancelRestaurantSLA(context.Context, string) error { return nil }

type testDispatchQueue struct{}

func (*testDispatchQueue) ScheduleFindCourier(context.Context, dispatchdomain.OrderID, dispatchdomain.Tier, time.Duration) error {
	return nil
}
func (*testDispatchQueue) ScheduleOfferTimeout(context.Context, dispatchdomain.OrderID, dispatchdomain.Tier, time.Duration) error {
	return nil
}

func request(t *testing.T, handler http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
func startPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "postgis/postgis:16-3.4", ExposedPorts: []string{"5432/tcp"}, Env: map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "mesob"}, WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(3 * time.Minute)}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = container.Terminate(stopCtx)
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("postgres://postgres:postgres@%s:%s/mesob?sslmode=disable", host, port.Port())
}
func startRedis(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForListeningPort("6379/tcp").WithStartupTimeout(2 * time.Minute)}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = container.Terminate(stopCtx)
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("redis://%s:%s/0", host, port.Port())
	clientOpts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(clientOpts)
	defer client.Close()
	for attempt := 0; attempt < 30; attempt++ {
		if client.Ping(ctx).Err() == nil {
			return url
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("redis container never became ready")
	return ""
}
func migrateReady(ctx context.Context, databaseURL, migrations string) error {
	var last error
	for attempt := 0; attempt < 60; attempt++ {
		last = pgadapter.RunMigrations(ctx, databaseURL, migrations)
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return last
}
