package main

import (
	"context"
	"fmt"
	"time"

	"go-micro.dev/v5/broker"
	"go-micro.dev/v5/logger"
	"go.uber.org/fx"

	jsbroker "go-micro-jetstream/pkg/broker"
)

func main() {
	app := fx.New(
		fx.Provide(NewBroker),
		fx.Invoke(SetupSubscriber),
		fx.Invoke(PublishTestMessages),
		fx.Invoke(RegisterLifecycle),
	)

	app.Run()
}

func NewBroker() (broker.Broker, error) {
	b := jsbroker.NewBroker(
		broker.Addrs("localhost:4222"),
		jsbroker.WithBatchSize(10),
		jsbroker.WithFetchWait(5*time.Second),
		jsbroker.WithClientName("validation-client"),
	)

	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to broker: %w", err)
	}

	return b, nil
}

func SetupSubscriber(lc fx.Lifecycle, b broker.Broker) error {
	handler := func(e broker.Event) error {
		msg := e.Message()
		logger.Infof("Received message: %s", string(msg.Body))
		return nil
	}

	sub, err := b.Subscribe(
		"test.messages",
		handler,
		broker.Queue("validation-queue"),
	)

	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			logger.Info("Unsubscribing...")
			return sub.Unsubscribe()
		},
	})

	return nil
}

func PublishTestMessages(lc fx.Lifecycle, b broker.Broker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			time.Sleep(2 * time.Second)

			logger.Info("Publishing test messages...")
			for i := 1; i <= 5; i++ {
				msg := &broker.Message{
					Body: []byte(fmt.Sprintf("Test message %d", i)),
				}

				if err := b.Publish("test.messages", msg); err != nil {
					logger.Errorf("Failed to publish message %d: %v", i, err)
				} else {
					logger.Infof("Published message %d", i)
				}

				time.Sleep(500 * time.Millisecond)
			}

			return nil
		},
	})
}

func RegisterLifecycle(lc fx.Lifecycle, b broker.Broker) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			logger.Info("Disconnecting broker...")
			return b.Disconnect()
		},
	})
}
