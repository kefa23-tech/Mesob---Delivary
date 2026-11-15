//go:build integration

package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mesob-delivery/backend/internal/realtime"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisHubDeliversTypedOrderEvents(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForListeningPort("6379/tcp").WithStartupTimeout(time.Minute)}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	defer container.Terminate(ctx)
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatal(err)
	}
	hub, err := New(fmt.Sprintf("redis://%s:%s/0", host, port.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	subctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := hub.SubscribeOrder(subctx, "order-123")
	if err != nil {
		t.Fatal(err)
	}
	want := realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: "order-123", RestaurantID: "restaurant-1", Status: "PREPARING"}
	if err := hub.Publish(ctx, want); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-events:
		if got.OrderID != want.OrderID || got.Type != want.Type || got.Status != want.Status || got.RestaurantID != want.RestaurantID {
			t.Fatalf("event mismatch: got %+v want %+v", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Redis Pub/Sub event")
	}
}
