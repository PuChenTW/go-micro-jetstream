# Project Overview: go-micro-jetstream

## Purpose
A production-ready NATS JetStream broker implementation for go-micro v5, providing reliable message publishing and pull-based consumption with built-in resilience and graceful shutdown.

## Tech Stack
- **Language**: Go 1.25.5
- **Libraries**:
  - `go-micro.dev/v5`: Core microservice framework.
  - `github.com/nats-io/nats.go`: NATS JetStream client.
  - `go.uber.org/fx`: Dependency injection framework.
  - `github.com/stretchr/testify`: Testing framework.

## Key Features
- **Pull-based subscriptions**: Background workers fetch messages in batches.
- **Synchronous publishing**: Guarantees persistence via JetStream acknowledgment.
- **Auto-creation**: Streams are automatically created on first publish.
- **Resilience**: Panic recovery, exponential backoff, and graceful shutdown.

## Codebase Structure
- `pkg/broker/`: Contains the core JetStream broker implementation.
  - `jetstream.go`: Main broker logic.
  - `event.go`: Implementation of go-micro broker events.
  - `options.go`: Custom JetStream broker options.
- `main.go`: A validation script demonstrating broker usage with `fx`.
- `docker-compose.yml`: Local setup for NATS with JetStream.
