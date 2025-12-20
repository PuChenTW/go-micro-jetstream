package jetstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"go-micro-jetstream/pkg/driver"
)

const (
	DefaultDrainWait = 100 * time.Millisecond
	DefaultBackoff   = 1 * time.Second
	MaxBackoff       = 30 * time.Second
)

type jetStreamBroker struct {
	nc        *nats.Conn
	js        jetstream.JetStream
	connected bool
	opts      Options
	subs      map[string]*subscriber
	mu        sync.RWMutex
}

type subscriber struct {
	id       string
	topic    string
	queue    string
	handler  driver.Handler
	consumer jetstream.Consumer
	cancel   context.CancelFunc
}

func (s *subscriber) Topic() string {
	return s.topic
}

func (s *subscriber) Unsubscribe(ctx context.Context) error {
	s.cancel()
	return nil
}

func NewBroker(opts ...Option) driver.Broker {
	options := defaultOptions()
	for _, o := range opts {
		o(&options)
	}

	return &jetStreamBroker{
		opts: options,
		subs: make(map[string]*subscriber),
	}
}

func (b *jetStreamBroker) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.connected {
		return nil
	}

	natsOpts := []nats.Option{
		nats.Name(b.opts.ClientName),
	}
	natsOpts = append(natsOpts, b.opts.NatsOptions...)

	nc, err := nats.Connect(strings.Join(b.opts.Addrs, ","), natsOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	b.nc = nc
	b.js = js
	b.connected = true

	b.opts.Logger.Printf("Connected to NATS at %s", nc.ConnectedUrl())
	return nil
}

func (b *jetStreamBroker) Disconnect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return nil
	}

	for _, sub := range b.subs {
		sub.cancel()
	}

	// Wait for processing to stop (simple sleep for now, could be better)
	time.Sleep(DefaultDrainWait)

	if err := b.nc.Drain(); err != nil {
		b.opts.Logger.Printf("Error draining NATS connection: %v", err)
	}

	b.connected = false
	b.subs = make(map[string]*subscriber)

	b.opts.Logger.Printf("Disconnected from NATS")
	return nil
}

func (b *jetStreamBroker) Publish(ctx context.Context, topic string, msg *driver.Message, opts ...driver.PublishOption) error {
	b.mu.RLock()
	connected := b.connected
	js := b.js
	b.mu.RUnlock()

	if !connected {
		return errors.New("broker not connected")
	}

	options := driver.PublishOptions{
		Context: ctx,
	}
	for _, o := range opts {
		o(&options)
	}

	if err := b.ensureStream(options.Context, topic); err != nil {
		return fmt.Errorf("failed to ensure stream: %w", err)
	}

	natsMsg := &nats.Msg{
		Subject: topic,
		Data:    msg.Body,
		Header:  nats.Header{},
	}
	for k, v := range msg.Header {
		natsMsg.Header.Set(k, v)
	}

	_, err := js.PublishMsg(options.Context, natsMsg)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

func (b *jetStreamBroker) Subscribe(ctx context.Context, topic string, h driver.Handler, opts ...driver.SubscribeOption) (driver.Subscriber, error) {
	b.mu.RLock()
	connected := b.connected
	js := b.js
	b.mu.RUnlock()

	if !connected {
		return nil, errors.New("broker not connected")
	}

	options := driver.SubscribeOptions{
		Context: ctx,
	}
	for _, o := range opts {
		o(&options)
	}

	durableName := options.Queue
	if durableName == "" {
		return nil, errors.New("queue name (durable consumer name) is required")
	}

	if err := validateDurableName(durableName); err != nil {
		return nil, fmt.Errorf("invalid queue name: %w", err)
	}

	streamName, err := b.getStreamForTopic(options.Context, topic)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream for topic: %w", err)
	}

	consumer, err := js.CreateOrUpdateConsumer(options.Context, streamName, jetstream.ConsumerConfig{
		Durable:        durableName,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: []string{topic},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	subCtx, cancel := context.WithCancel(context.Background())
	// Create a child context if the passed context is long-lived?
	// Actually, subscription loop should probably run until Unsubscribe or explicit cancel.
	// passed ctx in Subscribe is usually for the *setup* of subscription.
	// So creating a new background context for the loop is correct, controlled by cancel.

	subID := uuid.New().String()
	sub := &subscriber{
		id:       subID,
		topic:    topic,
		queue:    durableName,
		handler:  h,
		consumer: consumer,
		cancel:   cancel,
	}

	go b.runFetchLoop(subCtx, sub)

	b.mu.Lock()
	b.subs[subID] = sub
	b.mu.Unlock()

	b.opts.Logger.Printf("Subscribed to topic %s with durable consumer %s", topic, durableName)

	return sub, nil
}


func (b *jetStreamBroker) String() string {
	return "jetstream"
}

func (b *jetStreamBroker) ensureStream(ctx context.Context, topic string) error {
	streamName := streamNameFromTopic(topic)

	_, err := b.js.Stream(ctx, streamName)
	if err == nil {
		return nil
	}

	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}

	cfg := jetstream.StreamConfig{
		Name:      streamName,
		Subjects:  []string{fmt.Sprintf("%s.>", strings.ToLower(strings.Split(topic, ".")[0]))},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	}

	if b.opts.StreamConfig != nil {
		cfg = *b.opts.StreamConfig
		cfg.Name = streamName
	}

	_, err = b.js.CreateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create stream %s: %w", streamName, err)
	}

	b.opts.Logger.Printf("Created stream %s for topic %s", streamName, topic)

	return nil
}

func (b *jetStreamBroker) getStreamForTopic(ctx context.Context, topic string) (string, error) {
	streamName := streamNameFromTopic(topic)

	_, err := b.js.Stream(ctx, streamName)
	if err == nil {
		return streamName, nil
	}

	if err := b.ensureStream(ctx, topic); err != nil {
		return "", err
	}

	return streamName, nil
}

func (b *jetStreamBroker) runFetchLoop(ctx context.Context, sub *subscriber) {
	defer func() {
		if r := recover(); r != nil {
			b.opts.Logger.Printf("Panic in fetch loop for topic %s: %v", sub.topic, r)
		}
	}()

	backoff := DefaultBackoff
	maxBackoff := MaxBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := sub.consumer.Fetch(
			b.opts.BatchSize,
			jetstream.FetchMaxWait(b.opts.FetchWait),
		)

		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			b.opts.Logger.Printf("Fetch error for topic %s: %v", sub.topic, err)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}

		backoff = DefaultBackoff

		for msg := range msgs.Messages() {
			b.handleMessage(ctx, sub, msg)
		}

		if msgs.Error() != nil {
			b.opts.Logger.Printf("Message batch error: %v", msgs.Error())
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (b *jetStreamBroker) handleMessage(ctx context.Context, sub *subscriber, msg jetstream.Msg) {
	defer func() {
		if r := recover(); r != nil {
			b.opts.Logger.Printf("Panic in handler for topic %s: %v", sub.topic, r)
			msg.Nak()
		}
	}()

	driverMsg := &driver.Message{
		Topic:  sub.topic,
		Body:   msg.Data(),
		Header: make(map[string]string),
	}

	// Copy headers
	for k, v := range msg.Headers() {
		if len(v) > 0 {
			driverMsg.Header[k] = v[0]
		}
	}

	// We pass the fetch loop context to the handler?
	// Or should we create a new context with timeout?
	// The Handler signature is func(ctx, msg).
	// Let's pass the ctx derived from the fetch loop, which means if the subscription stops, the handler sees done.
	err := sub.handler(ctx, driverMsg)

	if err != nil {
		msg.Nak()
		return
	}

	msg.Ack()
}

func streamNameFromTopic(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) == 0 {
		return "DEFAULT"
	}
	return strings.ToUpper(strings.ReplaceAll(parts[0], "-", "_"))
}

func validateDurableName(name string) error {
	if name == "" {
		return errors.New("durable name cannot be empty")
	}
	// Simplified validation
	return nil
}
