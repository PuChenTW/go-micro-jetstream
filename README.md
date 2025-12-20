# NATS JetStream Driver for Go

A robust, context-aware NATS JetStream driver designed for modern Go applications, offering seamless `go-fx` integration and reliable message handling.

[繁體中文文檔](README.zh-TW.md)

## Design Philosophy

We believe that messaging infrastructure should be reliable, transparent, and easy to manage. This driver is built on three core pillars:

1.  **Context-Awareness**: Every operation respects `context.Context`. This ensures that your application handles timeouts, cancellation, and graceful shutdowns correctly, which is critical for microservices.
2.  **Explicit Lifecycle**: We treat the broker as a lifecycle-managed component. Connection and disconnection are explicit actions, making it perfect for use with dependency injection frameworks like `uber-go/fx`.
3.  **Reliability First**: Defaulting to **Pull Consumers** and **Synchronous Publishing** guarantees better flow control and data persistence. We prioritize "at-least-once" delivery over raw fire-and-forget throughput.

## Principles of Operation

### Pull-Based Consumption
Instead of NATS pushing messages to your service blindly, this driver uses **Pull Consumers**.
- **Batching**: Workers fetch messages in batches (configurable), reducing network chatter.
- **Backpressure**: The service only pulls what it can process, preventing overload during traffic spikes.
- **Control**: You can fine-tune `FetchWait` and `BatchSize` to balance latency and throughput.

### Durable Consumers
Queue groups are automatically mapped to JetStream **Durable Consumers**.
- **Load Balancing**: Multiple instances of your service with the same `queue` name will share the load.
- **Persistence**: NATS remembers the consumer's state. If all instances restart, processing resumes exactly where it left off.

### Synchronous Publishing
The `Publish` method waits for a positive acknowledgment (ACK) from the NATS server before returning. This ensures that when your function returns, the data is safely stored in the stream.

## Usage

### 1. Configuration (`Broker`)
The `Broker` is the central entry point. It requires a NATS address and offers tunables for performance.

```go
b := jetstream.NewBroker(
    jetstream.WithAddrs("nats://localhost:4222"),
    jetstream.WithBatchSize(10), // Optimize batch processing
    jetstream.WithLogger(log.Default()), // Optional: Custom logger
)
```

### 2. Lifecycle Management
Connect and disconnect should be tied to your application's lifecycle.

```go
// On Startup
if err := b.Connect(ctx); err != nil {
    log.Fatal(err)
}

// On Shutdown
if err := b.Disconnect(ctx); err != nil {
    log.Println("Error during disconnect:", err)
}
```

### 3. Publishing
Publishing is strictly synchronous to ensure data safety.

```go
msg := &driver.Message{Body: []byte("payload")}
err := b.Publish(ctx, "orders.created", msg)
```

### 4. Subscribing
Subscriptions require a **Subject** and a **Queue Name** (Durable Consumer).

```go
handler := func(ctx context.Context, msg *driver.Message) error {
    // Process message...
    return nil // Returning nil ACKs the message. Error will NAK.
}

// "orders.created" -> Subject
// "order-processor" -> Durable Consumer Name (Queue)
sub, err := b.Subscribe(ctx, "orders.created", handler, driver.WithQueue("order-processor"))
```

## Architecture

This driver implements a clean interface that decouples your business logic from the underlying NATS implementation.

- **`pkg/broker`**: Defines the clean, dependency-free interfaces (`Broker`, `Subscriber`, `Message`).
- **`pkg/broker/jetstream`**: The concrete implementation using the official NATS Go client.

This separation allows for easy mocking and testing of your business logic without needing a running NATS server.
