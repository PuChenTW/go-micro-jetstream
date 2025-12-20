package jetstream

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	driver "go-micro-jetstream/pkg/broker"

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

func TestDisconnectWait(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()

	addr := s.Addr().String()
	broker := NewBroker(WithAddrs(fmt.Sprintf("nats://%s", addr)))

	ctx := context.Background()
	if err := broker.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	topic := "test.disconnect.wait"

	// Channels for synchronization
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	handlerDone := make(chan struct{})

	var startOnce, doneOnce sync.Once
	handler := func(ctx context.Context, msg *driver.Message) error {
		startOnce.Do(func() { close(handlerStarted) })
		// Wait until allowed to proceed
		<-releaseHandler
		doneOnce.Do(func() { close(handlerDone) })
		return nil
	}

	_, err := broker.Subscribe(ctx, topic, handler, driver.WithQueue("test-queue"))
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	if err := broker.Publish(ctx, topic, &driver.Message{Body: []byte("trigger")}); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// 1. Wait for handler to start processing
	select {
	case <-handlerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for handler to start")
	}

	// 2. Start Disconnect in a separate goroutine
	disconnectDone := make(chan error, 1)
	go func() {
		disconnectDone <- broker.Disconnect(ctx)
	}()

	// 3. Verify Disconnect is blocked
	// Give it a moment to potentially fail (return early)
	select {
	case err := <-disconnectDone:
		t.Fatalf("Disconnect returned early (expected to block): %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: Disconnect should still be blocked waiting for handler
	}

	// 4. Release the handler
	close(releaseHandler)

	// 5. Verify Disconnect completes now
	select {
	case err := <-disconnectDone:
		if err != nil {
			t.Fatalf("Disconnect failed: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for Disconnect to complete after handler finished")
	}

	// Verify handler totally finished
	select {
	case <-handlerDone:
	default:
		t.Fatal("Handler should have finished")
	}
}
