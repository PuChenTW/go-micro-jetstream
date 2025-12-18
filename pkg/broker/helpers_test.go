package broker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go-micro.dev/v5/broker"
)

// testBrokerWithOptions creates a test broker with given options
func testBrokerWithOptions(opts ...broker.Option) broker.Broker {
	defaultOpts := []broker.Option{
		broker.Addrs("localhost:4222"),
		WithBatchSize(10),
		WithFetchWait(5 * time.Second),
	}

	allOpts := append(defaultOpts, opts...)
	return NewBroker(allOpts...)
}

// waitForMessages waits for N messages to be received within timeout
func waitForMessages(t *testing.T, messages chan *broker.Message, count int, timeout time.Duration) []*broker.Message {
	t.Helper()

	received := make([]*broker.Message, 0, count)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for i := 0; i < count; i++ {
		select {
		case msg := <-messages:
			received = append(received, msg)
		case <-timer.C:
			t.Fatalf("Timeout waiting for messages: expected %d, got %d", count, len(received))
			return received
		}
	}

	return received
}

// publishTestMessage publishes a test message and handles errors
func publishTestMessage(t *testing.T, b broker.Broker, topic string, body []byte, headers map[string]string) error {
	t.Helper()

	msg := &broker.Message{
		Header: headers,
		Body:   body,
	}

	return b.Publish(topic, msg)
}

// cleanupStreams removes all test streams from NATS
func cleanupStreams(t *testing.T, js jetstream.JetStream) error {
	t.Helper()

	ctx := context.Background()
	streams := js.ListStreams(ctx)

	for s := range streams.Info() {
		if strings.HasPrefix(s.Config.Name, "TEST") || strings.HasPrefix(s.Config.Name, "ORDERS") {
			if err := js.DeleteStream(ctx, s.Config.Name); err != nil {
				return fmt.Errorf("failed to delete stream %s: %w", s.Config.Name, err)
			}
		}
	}

	return nil
}

// cleanupConsumers removes all consumers for a stream
func cleanupConsumers(t *testing.T, js jetstream.JetStream, streamName string) error {
	t.Helper()

	ctx := context.Background()
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return fmt.Errorf("failed to get stream %s: %w", streamName, err)
	}

	consumers := stream.ListConsumers(ctx)
	for c := range consumers.Info() {
		if err := js.DeleteConsumer(ctx, streamName, c.Name); err != nil {
			return fmt.Errorf("failed to delete consumer %s: %w", c.Name, err)
		}
	}

	return nil
}

// createTestStream creates a stream for testing
func createTestStream(t *testing.T, js jetstream.JetStream, topic string) string {
	t.Helper()

	streamName := streamNameFromTopic(topic)
	ctx := context.Background()

	cfg := jetstream.StreamConfig{
		Name:      streamName,
		Subjects:  []string{fmt.Sprintf("%s.>", strings.ToLower(strings.Split(topic, ".")[0]))},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	}

	_, err := js.CreateStream(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create test stream %s: %v", streamName, err)
	}

	return streamName
}

// connectWithRetry connects to NATS with retry logic
func connectWithRetry(t *testing.T, b broker.Broker, maxRetries int, delay time.Duration) error {
	t.Helper()

	var err error
	for i := 0; i < maxRetries; i++ {
		err = b.Connect()
		if err == nil {
			return nil
		}

		if i < maxRetries-1 {
			time.Sleep(delay)
		}
	}

	return fmt.Errorf("failed to connect after %d retries: %w", maxRetries, err)
}

// getNATSJetStream creates a direct NATS JetStream connection for test utilities
func getNATSJetStream(t *testing.T) (jetstream.JetStream, *nats.Conn) {
	t.Helper()

	nc, err := nats.Connect("localhost:4222")
	if err != nil {
		t.Fatalf("Failed to connect to NATS: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		t.Fatalf("Failed to create JetStream context: %v", err)
	}

	return js, nc
}

// assertMessageBody checks if message body matches expected value
func assertMessageBody(t *testing.T, msg *broker.Message, expected string) {
	t.Helper()

	if string(msg.Body) != expected {
		t.Errorf("Expected body %q, got %q", expected, string(msg.Body))
	}
}

// assertMessageHeader checks if message header has expected value
func assertMessageHeader(t *testing.T, msg *broker.Message, key, expected string) {
	t.Helper()

	actual, ok := msg.Header[key]
	if !ok {
		t.Errorf("Expected header %q not found", key)
		return
	}

	if actual != expected {
		t.Errorf("Expected header %q to be %q, got %q", key, expected, actual)
	}
}
