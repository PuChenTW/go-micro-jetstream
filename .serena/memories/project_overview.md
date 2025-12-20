# Project Overview: go-micro-jetstream

## Purpose
A production-ready NATS JetStream driver for Go, providing reliable message publishing and pull-based consumption. It is designed for seamless integration with `go-fx` and uses a context-aware `driver.Broker` interface, removing the dependency on `go-micro`.

## Tech Stack
- **Language**: Go 1.25.5
- **Libraries**:
  - `github.com/nats-io/nats.go`: NATS JetStream client.
  - `go.uber.org/fx`: Dependency injection framework used for lifecycle management.
  - `github.com/stretchr/testify`: Testing framework.
  - `github.com/nats-io/nats-server/v2`: In-memory NATS server for unit tests.

## Key Features
- **Context-aware Interface**: All broker methods accept `context.Context`.
- **Pull-based subscriptions**: Background workers fetch messages in batches.
- **Synchronous publishing**: Guarantees persistence via JetStream acknowledgment.
- **Auto-creation**: Streams are automatically created on first publish/subscribe.
- **Resilience**: Panic recovery, exponential backoff, and graceful shutdown.
- **No Heavy Framework Dependency**: Decoupled from `go-micro`, allowing for lighter weight integration.

## Codebase Structure
- `pkg/driver/`: Contains the broker interface and implementations.
  - `broker.go`: Defines the `Broker`, `Subscriber`, and `Message` interfaces.
  - `jetstream/`: NATS JetStream implementation of the driver.
- `main.go`: A validation script demonstrating broker usage with `fx`.
- `docker-compose.yml`: Local setup for NATS with JetStream.
