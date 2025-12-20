package driver

import (
	"context"
)

// Message represents a message to be published or received
type Message struct {
	Header map[string]string
	Body   []byte
	Topic  string
}

// Handler is the function signature for message handlers
type Handler func(ctx context.Context, msg *Message) error

// Subscriber represents a subscription to a topic
type Subscriber interface {
	Topic() string
	Unsubscribe(ctx context.Context) error
}

// Broker is the interface for message communication
type Broker interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Publish(ctx context.Context, topic string, msg *Message, opts ...PublishOption) error
	Subscribe(ctx context.Context, topic string, h Handler, opts ...SubscribeOption) (Subscriber, error)
	String() string
}

// PublishOptions contains options for publishing
type PublishOptions struct {
	Context context.Context
}

// PublishOption sets a publish option
type PublishOption func(*PublishOptions)

// SubscribeOptions contains options for subscribing
type SubscribeOptions struct {
	Context context.Context
	Queue   string
}

// SubscribeOption sets a subscribe option
type SubscribeOption func(*SubscribeOptions)

// WithQueue sets the queue name for subscription
func WithQueue(name string) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.Queue = name
	}
}
