package main

import (
	"context"
	"fmt"
	"time"

	"log"

	"go.uber.org/fx"

	"go-micro-jetstream/pkg/broker"
	"go-micro-jetstream/pkg/broker/jetstream"
)

func main() {
	app := fx.New(
		fx.Provide(NewBroker),
		fx.Invoke(SetupSubscriber),
		fx.Invoke(PublishTestMessages),
	)

	app.Run()
}

func NewBroker(lc fx.Lifecycle) (broker.Broker, error) {
	b := jetstream.NewBroker(
		jetstream.WithAddrs("localhost:4222"),
		jetstream.WithBatchSize(10),
		jetstream.WithFetchWait(5*time.Second),
		jetstream.WithClientName("validation-client"),
	)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Printf("Connecting broker...")
			return b.Connect(ctx)
		},
		OnStop: func(ctx context.Context) error {
			log.Printf("Disconnecting broker...")
			return b.Disconnect(ctx)
		},
	})

	return b, nil
}

func SetupSubscriber(lc fx.Lifecycle, b broker.Broker) {
	var sub broker.Subscriber

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			handler := func(ctx context.Context, msg *broker.Message) error {
				log.Printf("Received message: %s", string(msg.Body))
				return nil
			}

			log.Printf("Subscribing...")
			s, err := b.Subscribe(
				ctx,
				"test.messages",
				handler,
				broker.WithQueue("validation-queue"),
			)
			if err != nil {
				return fmt.Errorf("failed to subscribe: %w", err)
			}
			sub = s
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if sub != nil {
				log.Printf("Unsubscribing...")
				return sub.Unsubscribe(ctx)
			}
			return nil
		},
	})
}

func PublishTestMessages(lc fx.Lifecycle, b broker.Broker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Run in background to not block OnStart
			go func() {
				time.Sleep(2 * time.Second)

				log.Printf("Publishing test messages...")
				// Context for publishing
				bgCtx := context.Background()

				for i := 1; i <= 5; i++ {
					msg := &broker.Message{
						Body: fmt.Appendf(nil, "Test message %d", i),
					}

					if err := b.Publish(bgCtx, "test.messages", msg); err != nil {
						log.Printf("Failed to publish message %d: %v", i, err)
					} else {
						log.Printf("Published message %d", i)
					}

					time.Sleep(500 * time.Millisecond)
				}
			}()
			return nil
		},
	})
}
