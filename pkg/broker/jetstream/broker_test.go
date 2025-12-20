package jetstream

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	bk "go-micro-jetstream/pkg/broker"

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
	msgCh := make(chan *bk.Message, 1)

	handler := func(ctx context.Context, msg *bk.Message) error {
		msgCh <- msg
		return nil
	}

	_, err := broker.Subscribe(ctx, topic, handler, bk.WithQueue("test-queue"))
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	testMsg := &bk.Message{
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
	handler := func(ctx context.Context, msg *bk.Message) error {
		startOnce.Do(func() { close(handlerStarted) })
		// Wait until allowed to proceed
		<-releaseHandler
		doneOnce.Do(func() { close(handlerDone) })
		return nil
	}

	_, err := broker.Subscribe(ctx, topic, handler, bk.WithQueue("test-queue"))
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	if err := broker.Publish(ctx, topic, &bk.Message{Body: []byte("trigger")}); err != nil {
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

type mockLogger struct {
	mu   sync.Mutex
	logs []string
}

func (m *mockLogger) Printf(format string, v ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf(format, v...))
}

func (m *mockLogger) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.logs...) // Copy
}

func TestLoggerUsage(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()

	addr := s.Addr().String()
	logger := &mockLogger{}
	broker := NewBroker(
		WithAddrs(fmt.Sprintf("nats://%s", addr)),
		WithLogger(logger),
	)

	ctx := context.Background()
	if err := broker.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer broker.Disconnect(ctx)

	// 1. Test Publish Error Logic (simulate by publishing to invalid subject if possible, or just verifying clean logs so far)
	// JetStream publish failures are hard to force without network issues, but we can verify successful connect log.
	logs := logger.Logs()
	if len(logs) == 0 {
		t.Fatal("Expected logs from Connect, got none")
	}

	foundConnect := slices.Contains(logs, fmt.Sprintf("Connected to NATS at nats://%s", addr))
	if !foundConnect {
		t.Errorf("Expected connect log, got: %v", logs)
	}

	// 2. Test Handler Error Logging
	topic := "test.logger.handler.error"
	handlerErr := fmt.Errorf("simulated handler error")
	handler := func(ctx context.Context, msg *bk.Message) error {
		return handlerErr
	}

	_, err := broker.Subscribe(ctx, topic, handler, bk.WithQueue("test-queue"))
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	if err := broker.Publish(ctx, topic, &bk.Message{Body: []byte("fail")}); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for handler error log
	var foundHandlerErr bool
	for range 20 {
		logs = logger.Logs()
		for _, l := range logs {
			expected := fmt.Sprintf("Handler error for topic %s: %v", topic, handlerErr)
			if l == expected {
				foundHandlerErr = true
				break
			}
		}
		if foundHandlerErr {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !foundHandlerErr {
		t.Errorf("Timeout waiting for handler error log. Logs: %v", logs)
	}
}

func TestUnsubscribe(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()

	addr := s.Addr().String()
	broker := NewBroker(WithAddrs(fmt.Sprintf("nats://%s", addr)))

	ctx := context.Background()
	if err := broker.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer broker.Disconnect(ctx)

	topic := "test.unsubscribe"
	handler := func(ctx context.Context, msg *bk.Message) error {
		return nil
	}

	sub, err := broker.Subscribe(ctx, topic, handler, bk.WithQueue("test-queue"))
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Assert subscriber is in the map
	jsBroker, ok := broker.(*jetStreamBroker)
	if !ok {
		t.Fatal("Broker is not jetStreamBroker")
	}

	jsBroker.mu.RLock()
	if len(jsBroker.subs) != 1 {
		t.Errorf("Expected 1 subscriber, got %d", len(jsBroker.subs))
	}
	jsBroker.mu.RUnlock()

	if err := sub.Unsubscribe(ctx); err != nil {
		t.Fatalf("Failed to unsubscribe: %v", err)
	}

	// Assert subscriber is removed from map
	jsBroker.mu.RLock()
	if len(jsBroker.subs) != 0 {
		t.Errorf("Expected 0 subscribers, got %d", len(jsBroker.subs))
	}
	jsBroker.mu.RUnlock()
}
