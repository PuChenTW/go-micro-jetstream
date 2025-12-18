# Suggested Commands for go-micro-jetstream

## Environment Setup
```bash
# Start NATS with JetStream
docker-compose up -d
```

## Development Commands
```bash
# Run all tests
go test -v ./...

# Build the validation entrypoint
go build -o jetstream-broker

# Run the validation script
./jetstream-broker

# Format code
go fmt ./...

# Run go vet
go vet ./...
```

## NATS Inspection
```bash
# Check JetStream streams
curl http://localhost:8222/jsz?streams=true

# Check JetStream consumers
curl http://localhost:8222/jsz?consumers=true
```
