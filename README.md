# NATS JetStream Broker for go-micro v5

A production-ready NATS JetStream broker implementation for go-micro v5, providing reliable message publishing and pull-based consumption with built-in resilience and graceful shutdown.

[繁體中文文檔](README.zh-TW.md)

## Overview

This broker integrates NATS JetStream's persistent messaging capabilities with go-micro's broker interface, enabling reliable event-driven microservices communication. It uses pull-based consumers with batch processing for optimal throughput and backpressure management.

## Key Features

### Core Functionality
- **Pull-based subscriptions**: Background workers fetch messages in configurable batches
- **Synchronous publishing**: Waits for JetStream acknowledgment to guarantee persistence
- **Stream auto-creation**: Automatically creates streams on first publish with sensible defaults
- **Durable consumers**: Queue names map to JetStream durable consumers for load balancing
- **Message ordering**: Sequential processing within batches maintains per-consumer ordering

### Production Standards
- **Concurrency safety**: `sync.RWMutex` protects broker state across goroutines
- **Panic recovery**: Two-level recovery (fetch loop + handler) prevents worker crashes
- **Exponential backoff**: Fetch errors trigger 1s → 30s backoff to prevent tight loops
- **Graceful shutdown**: `nc.Drain()` waits for in-flight messages before disconnecting
- **Explicit acknowledgment**: Handlers control Ack/Nak based on processing result

## Installation

```bash
go get go-micro-jetstream
```

### Dependencies
- `go-micro.dev/v5` - Broker interface
- `github.com/nats-io/nats.go` - NATS JetStream client
- `github.com/google/uuid` - Subscription ID generation

## Quick Start

### 1. Start NATS with JetStream

```bash
# Using Docker
docker run -d --name nats-jetstream \
  -p 4222:4222 -p 8222:8222 \
  nats:latest -js -m 8222

# Or native binary
nats-server -js -m 8222
```

### 2. Create a Broker

```go
package main

import (
    "time"
    "go-micro.dev/v5/broker"
    jsbroker "go-micro-jetstream/pkg/broker"
)

func main() {
    b := jsbroker.NewBroker(
        broker.Addrs("localhost:4222"),
        jsbroker.WithBatchSize(10),
        jsbroker.WithFetchWait(5*time.Second),
    )

    if err := b.Connect(); err != nil {
        panic(err)
    }
    defer b.Disconnect()
}
```

### 3. Subscribe to Messages

```go
handler := func(e broker.Event) error {
    msg := e.Message()
    fmt.Printf("Received: %s\n", string(msg.Body))
    return nil  // Ack on success, return error to Nak
}

sub, err := b.Subscribe(
    "orders.created",
    handler,
    broker.Queue("order-processor"),  // Durable consumer name
)
if err != nil {
    panic(err)
}
defer sub.Unsubscribe()
```

### 4. Publish Messages

```go
msg := &broker.Message{
    Header: map[string]string{"type": "order"},
    Body:   []byte(`{"id": "123", "amount": 99.99}`),
}

if err := b.Publish("orders.created", msg); err != nil {
    panic(err)
}
```

## Configuration Options

### Broker Options

```go
// NATS server addresses
broker.Addrs("nats://localhost:4222")

// Batch size for Fetch operations (default: 10)
jsbroker.WithBatchSize(20)

// Fetch timeout (default: 5s)
jsbroker.WithFetchWait(3 * time.Second)

// NATS client name (default: go-micro-{hostname})
jsbroker.WithClientName("my-service")

// Custom NATS options for TLS/Auth
jsbroker.WithNATSOptions(
    nats.UserInfo("user", "password"),
    nats.RootCAs("./certs/ca.pem"),
)

// Custom stream configuration template
jsbroker.WithStreamConfig(jetstream.StreamConfig{
    Retention: jetstream.LimitsPolicy,
    MaxAge:    24 * time.Hour,
})
```

### Subscribe Options

```go
// Durable consumer for load balancing
broker.Queue("my-consumer-group")

// Custom context for cancellation
broker.SubscribeContext(ctx)
```

### Publish Options

```go
// Custom context with timeout
broker.PublishContext(ctx)
```

## Architecture

### Stream Naming Convention

Topics are mapped to streams using the first segment:

```
orders.created     → ORDERS stream
orders.updated     → ORDERS stream
user-events.login  → USER_EVENTS stream
```

Streams are auto-created with:
- **Retention**: WorkQueue (messages deleted after consumption)
- **Storage**: File (persistent)
- **Subjects**: `{prefix}.>` wildcard pattern

### Pull Consumer Worker Pattern

Each subscription spawns a dedicated goroutine:

```
┌─────────────────────────────────────┐
│  Subscription Worker Goroutine      │
│                                     │
│  Loop:                              │
│    1. Fetch(batchSize) from JetStream │
│    2. For each message:             │
│       - Wrap as broker.Event        │
│       - Call handler (with recover) │
│       - Ack on success, Nak on error│
│    3. Exponential backoff on errors │
│    4. Check context cancellation    │
└─────────────────────────────────────┘
```

Benefits:
- Natural backpressure through batch fetching
- Clean shutdown via context cancellation
- Independent error handling per subscription

### Durable Consumers

Queue names map directly to JetStream durable consumer names:

```go
// Multiple instances with same queue share message delivery
broker.Queue("order-processor")  // Creates durable consumer "order-processor"
```

This enables:
- Load balancing across service instances
- Consumer state persistence across restarts
- Message replay from last acknowledged position

## Error Handling

### Handler Errors

Return an error to trigger message redelivery:

```go
handler := func(e broker.Event) error {
    if err := processMessage(e.Message()); err != nil {
        // Message will be Nak'd and redelivered
        return err
    }
    return nil  // Message will be Ack'd
}
```

### Handler Panics

Panics are recovered and logged. The message is Nak'd for redelivery:

```go
handler := func(e broker.Event) error {
    panic("oops")  // Recovered, logged, message Nak'd
}
```

### Fetch Errors

Network or JetStream errors trigger exponential backoff:

```
Error → Wait 1s → Retry
Error → Wait 2s → Retry
Error → Wait 4s → Retry
...
Error → Wait 30s → Retry (max)
```

Success resets backoff to 1s.

## Testing

### Run Validation Script

The included validation script demonstrates full functionality:

```bash
# Ensure NATS with JetStream is running
docker run -d -p 4222:4222 -p 8222:8222 nats:latest -js -m 8222

# Build and run
go build -o jetstream-broker
./jetstream-broker
```

Expected output:
```
Connected to NATS at nats://localhost:4222
Created stream TEST for topic test.messages
Subscribed to topic test.messages with durable consumer validation-queue
Publishing test messages...
Published message 1
Received message: Test message 1
Published message 2
Received message: Test message 2
...
```

### Verify JetStream State

```bash
# Check streams
curl http://localhost:8222/jsz?streams=true

# Check consumers
curl http://localhost:8222/jsz?consumers=true
```

## Production Considerations

### Idempotency

JetStream guarantees at-least-once delivery. Design handlers to be idempotent:

```go
handler := func(e broker.Event) error {
    // Check if already processed
    if alreadyProcessed(e.Message().Header["message-id"]) {
        return nil  // Ack without reprocessing
    }

    // Process and mark as complete
    return process(e.Message())
}
```

### Message Ordering

Ordering is guaranteed per-consumer, not globally:
- Messages to the same consumer arrive in order
- Multiple consumers may process messages in different orders
- Use message IDs or timestamps for cross-consumer ordering

### Consumer State Cleanup

Durable consumers persist after `Unsubscribe()`. To delete:

```bash
# Using NATS CLI
nats consumer delete STREAM_NAME CONSUMER_NAME

# Or via HTTP API
curl -X DELETE http://localhost:8222/jsapi/v1/streams/TEST/consumers/validation-queue
```

### Monitoring

Key metrics to track:
- Messages published (counter)
- Messages consumed (counter)
- Handler errors (counter)
- Handler panics (counter)
- Fetch errors (counter)
- Active subscriptions (gauge)

Check JetStream monitoring:
```bash
curl http://localhost:8222/jsz
```

### Performance Tuning

**High throughput**: Increase batch size
```go
jsbroker.WithBatchSize(100)
```

**Low latency**: Decrease fetch wait time
```go
jsbroker.WithFetchWait(1 * time.Second)
```

**Large messages**: Use byte-based batching (future enhancement)

## Design Principles

This implementation follows Linus Torvalds' software philosophy:

1. **Simplicity over cleverness**: Direct NATS types, no unnecessary abstractions
2. **Data structures first**: State machine built around clear broker struct
3. **Explicit error handling**: No silent failures, all errors logged and propagated
4. **Performance matters**: Batch fetching, minimal allocations, efficient locking
5. **Good taste in code**: Pull workers are obvious, not clever

## Limitations

- Pull consumers only (no push consumer support)
- Synchronous publish only (no async batching)
- Auto-created streams use fixed naming convention
- No built-in metrics/tracing (add via middleware)

## License

MIT

## Contributing

Pull requests welcome. Ensure:
- No emojis in code or logs
- Go best practices followed
- Production-ready error handling
- Tests pass
- Documentation updated

## See Also

- [NATS JetStream Documentation](https://docs.nats.io/nats-concepts/jetstream)
- [go-micro Documentation](https://go-micro.dev)
- [Example Services](./examples/) (if available)
