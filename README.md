# NATS JetStream Driver for Go

A production-ready NATS JetStream driver, designed for `go-fx` integration and providing reliable message publishing and pull-based consumption.

[繁體中文文檔](README.zh-TW.md)

## Overview

This driver connects NATS JetStream with your Go application, enabling reliable event-driven microservices communication. It uses pull-based consumers with batch processing for optimal throughput and backpressure management, and exposes a context-aware interface compatible with modern Go practices.

## Key Features

- **Context-aware Interface**: All methods (`Connect`, `Publish`, `Subscribe`) accept `context.Context` for cancellation and tracing.
- **go-fx Integration**: Designed to work seamlessly with `go.uber.org/fx` for lifecycle management.
- **Pull-based Subscriptions**: Background workers fetch messages in configurable batches.
- **Synchronous Publishing**: Waits for JetStream acknowledgment to guarantee persistence.
- **Stream Auto-creation**: Automatically creates streams on first publish/subscribe with sensible defaults.
- **Durable Consumers**: Queue names map to JetStream durable consumers for load balancing.

## Installation

```bash
go get go-micro-jetstream
```

### Dependencies
- `github.com/nats-io/nats.go` - NATS JetStream client
- `go.uber.org/fx` - Dependency injection framework (optional but recommended)

## Quick Start

### 1. Start NATS with JetStream

```bash
docker run -d --name nats-jetstream \
  -p 4222:4222 -p 8222:8222 \
  nats:latest -js -m 8222
```

### 2. Define the Application (using go-fx)

```go
package main

import (
	"context"
	"log"
	"time"

	"go.uber.org/fx"
	"go-micro-jetstream/pkg/driver"
	"go-micro-jetstream/pkg/driver/jetstream"
)

func main() {
	app := fx.New(
		fx.Provide(NewBroker),
		fx.Invoke(SetupSubscriber),
		fx.Invoke(PublishMessages),
	)
	app.Run()
}
```

### 3. Create a Broker Provider

```go
func NewBroker(lc fx.Lifecycle) (driver.Broker, error) {
	b := jetstream.NewBroker(
		jetstream.WithAddrs("localhost:4222"),
		jetstream.WithBatchSize(10),
		jetstream.WithFetchWait(5*time.Second),
		jetstream.WithClientName("my-service"),
        // Optional: Inject custom logger
        // jetstream.WithLogger(myLogger),
	)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Println("Connecting broker...")
			return b.Connect(ctx)
		},
		OnStop: func(ctx context.Context) error {
			log.Println("Disconnecting broker...")
			return b.Disconnect(ctx)
		},
	})

	return b, nil
}
```

### 4. Subscribe to Messages

```go
func SetupSubscriber(lc fx.Lifecycle, b driver.Broker) {
    var sub driver.Subscriber

    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            handler := func(ctx context.Context, msg *driver.Message) error {
                log.Printf("Received: %s", string(msg.Body))
                return nil // Ack on success, error will Nak
            }

            s, err := b.Subscribe(
                ctx,
                "orders.created",
                handler,
                driver.WithQueue("order-processor"), // Required: Durable consumer name
            )
            if err != nil {
                return err
            }
            sub = s
            return nil
        },
        OnStop: func(ctx context.Context) error {
            if sub != nil {
                return sub.Unsubscribe(ctx)
            }
            return nil
        },
    })
}
```

### 5. Publish Messages

```go
func PublishMessages(lc fx.Lifecycle, b driver.Broker) {
    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            go func() {
                msg := &driver.Message{
                    Body: []byte(`{"id": 123}`),
                }
                if err := b.Publish(context.Background(), "orders.created", msg); err != nil {
                    log.Printf("Publish failed: %v", err)
                }
            }()
            return nil
        },
    })
}
```

## Configuration Options

### Broker Options (`pkg/driver/jetstream`)

```go
// NATS server addresses
jetstream.WithAddrs("nats://localhost:4222")

// Batch size for Fetch operations (default: 10)
jetstream.WithBatchSize(20)

// Fetch timeout (default: 5s)
jetstream.WithFetchWait(3 * time.Second)

// NATS client name (default: go-micro-{hostname})
jetstream.WithClientName("my-service")

// Custom Logger
jetstream.WithLogger(myLogger)

// Custom NATS options for TLS/Auth
jetstream.WithNatsOptions(
    nats.UserInfo("user", "password"),
)
```

### Subscribe Options (`pkg/driver`)

```go
// Durable consumer name (REQUIRED)
driver.WithQueue("my-consumer-group")
```

## Architecture

This driver implements a clean `driver.Broker` interface found in `pkg/driver/broker.go`. The implementation is in `pkg/driver/jetstream`.

- **Subscriptions**: Each subscription runs a dedicated goroutine that pulls messages from JetStream.
- **Panic Recovery**: Handlers are protected by panic recovery to prevent worker crashes.
- **Connection Management**: Connection logic is separate from instance creation, allowing for better control via dependency injection lifecycles.

## Changes from Legacy `go-micro` Broker

- **Package**: Moved from `pkg/broker` to `pkg/driver` + `pkg/driver/jetstream`.
- **Interface**: Uses `context.Context` in all methods.
- **Dependency**: No longer depends on `go-micro.dev/v5`.
- **Logging**: Uses standard `log` or injected logger instead of `go-micro/logger`.
