package jetstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go-micro-jetstream/pkg/driver"

	"github.com/nats-io/nats-server/v2/server"
)

func runServer(t *testing.T) *server.Server {
	t.Helper()
	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // random port
		JetStream: true,
	}
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("Error creating nats server: %v", err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatalf("Nats server did not start")
	}
	return s
}

func TestConnectDisconnect(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()

	addr := s.Addr().String()
	broker := NewBroker(WithAddrs(fmt.Sprintf("nats://%s", addr)))

	ctx := context.Background()
	if err := broker.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	if err := broker.Disconnect(ctx); err != nil {
		t.Fatalf("Failed to disconnect: %v", err)
	}
}

func TestPublishSubscribe(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()

	addr := s.Addr().String()
	broker := NewBroker(WithAddrs(fmt.Sprintf("nats://%s", addr)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := broker.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer broker.Disconnect(ctx)

	topic := "test.topic"
	msgCh := make(chan *driver.Message, 1)

	handler := func(ctx context.Context, msg *driver.Message) error {
		msgCh <- msg
		return nil
	}

	_, err := broker.Subscribe(ctx, topic, handler, driver.WithQueue("test-queue"))
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	testMsg := &driver.Message{
		Header: map[string]string{"Key": "Value"},
		Body:   []byte("hello world"),
	}

	if err := broker.Publish(ctx, topic, testMsg); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	select {
	case received := <-msgCh:
		if string(received.Body) != string(testMsg.Body) {
			t.Errorf("Expected body %s, got %s", testMsg.Body, received.Body)
		}
		if received.Header["Key"] != "Value" {
			t.Errorf("Expected header Value, got %s", received.Header["Key"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for message")
	}
}
