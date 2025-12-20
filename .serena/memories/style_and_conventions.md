# Style and Conventions: go-micro-jetstream

## Core Philosophy (Linus Torvalds Style)
- **Ruthless Simplicity**: No unnecessary abstractions. Concrete > Abstract.
- **Performance-First**: Batch fetching, minimal allocations, efficient locking.
- **Good Taste**: Obvious code over clever code.

## Coding Standards
- **Naming**: Descriptive names. Short for local, long for public.
- **Functions**: Do one thing. Return early. Handle errors explicitly.
- **Error Handling**: Fail fast. Clean up resources in reverse order (e.g., in `Disconnect` or `OnStop`).
- **No Emojis**: Do not use emojis in code, comments, or logs.
- **No Magic Numbers**: Use named constants for timeouts, batch sizes, and configuration defaults.
- **Concurrency**: Use `sync.RWMutex` for state protection. Ensure safety across goroutines.

## Architecture Patterns
- **Pull Consumers**: Use pull-based worker patterns for natural backpressure.
- **Exponential Backoff**: Implement backoff (e.g., 1s to 30s) for fetch errors.
- **Graceful Shutdown**: Use `nc.Drain()` and context cancellation for clean exits.
- **Explicit Ack/Nak**: Control message acknowledgement based on processing results.
