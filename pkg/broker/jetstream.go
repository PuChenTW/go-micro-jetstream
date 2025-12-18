package broker

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
	"go-micro.dev/v5/broker"
	"go-micro.dev/v5/logger"
)

type jetStreamBroker struct {
	nc        *nats.Conn
	js        jetstream.JetStream
	connected bool
	opts      broker.Options
	jsOpts    jetStreamOptions
	subs      map[string]*jsSubscriber
	mu        sync.RWMutex
}

func NewBroker(opts ...broker.Option) broker.Broker {
	options := broker.Options{
		Context: context.Background(),
	}

	for _, o := range opts {
		o(&options)
	}

	return &jetStreamBroker{
		opts:   options,
		jsOpts: getJetStreamOptions(&options),
		subs:   make(map[string]*jsSubscriber),
	}
}

func (b *jetStreamBroker) Init(opts ...broker.Option) error {
	for _, o := range opts {
		o(&b.opts)
	}

	b.jsOpts = getJetStreamOptions(&b.opts)

	return nil
}

func (b *jetStreamBroker) Connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.connected {
		return nil
	}

	addrs := b.opts.Addrs
	if len(addrs) == 0 {
		addrs = []string{nats.DefaultURL}
	}

	natsOpts := []nats.Option{
		nats.Name(b.jsOpts.clientName),
	}
	natsOpts = append(natsOpts, b.jsOpts.natsOpts...)

	nc, err := nats.Connect(strings.Join(addrs, ","), natsOpts...)
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

	logger.Infof("Connected to NATS at %s", nc.ConnectedUrl())

	return nil
}

func (b *jetStreamBroker) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return nil
	}

	for _, sub := range b.subs {
		sub.cancel()
	}

	time.Sleep(100 * time.Millisecond)

	if err := b.nc.Drain(); err != nil {
		logger.Errorf("Error draining NATS connection: %v", err)
	}

	b.connected = false
	b.subs = make(map[string]*jsSubscriber)

	logger.Info("Disconnected from NATS")

	return nil
}

func (b *jetStreamBroker) Publish(topic string, msg *broker.Message, opts ...broker.PublishOption) error {
	b.mu.RLock()
	connected := b.connected
	js := b.js
	b.mu.RUnlock()

	if !connected {
		return errors.New("broker not connected")
	}

	options := broker.PublishOptions{
		Context: context.Background(),
	}
	for _, o := range opts {
		o(&options)
	}

	if err := b.ensureStream(options.Context, topic); err != nil {
		return fmt.Errorf("failed to ensure stream: %w", err)
	}

	body, headers := marshalMessage(msg)

	natsMsg := &nats.Msg{
		Subject: topic,
		Data:    body,
		Header:  headers,
	}

	_, err := js.PublishMsg(options.Context, natsMsg)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

func (b *jetStreamBroker) Subscribe(topic string, h broker.Handler, opts ...broker.SubscribeOption) (broker.Subscriber, error) {
	b.mu.RLock()
	connected := b.connected
	js := b.js
	b.mu.RUnlock()

	if !connected {
		return nil, errors.New("broker not connected")
	}

	options := broker.SubscribeOptions{
		Context: context.Background(),
	}
	for _, o := range opts {
		o(&options)
	}

	queue := options.Queue
	durableName := queue
	if durableName == "" {
		durableName = fmt.Sprintf("%s-%s", sanitizeTopicForDurable(topic), uuid.New().String()[:8])
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

	ctx, cancel := context.WithCancel(context.Background())

	subID := uuid.New().String()
	sub := &jsSubscriber{
		id:       subID,
		topic:    topic,
		queue:    queue,
		handler:  h,
		consumer: consumer,
		cancel:   cancel,
		opts:     options,
	}

	go b.runFetchLoop(ctx, sub)

	b.mu.Lock()
	b.subs[subID] = sub
	b.mu.Unlock()

	logger.Infof("Subscribed to topic %s with durable consumer %s", topic, durableName)

	return sub, nil
}

func (b *jetStreamBroker) Options() broker.Options {
	return b.opts
}

func (b *jetStreamBroker) Address() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.nc != nil && b.connected {
		return b.nc.ConnectedUrl()
	}

	return ""
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

	if b.jsOpts.streamConfig != nil {
		cfg = *b.jsOpts.streamConfig
		cfg.Name = streamName
	}

	_, err = b.js.CreateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create stream %s: %w", streamName, err)
	}

	logger.Infof("Created stream %s for topic %s", streamName, topic)

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

func (b *jetStreamBroker) runFetchLoop(ctx context.Context, sub *jsSubscriber) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("Panic in fetch loop for topic %s: %v", sub.topic, r)
		}
	}()

	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := sub.consumer.Fetch(
			b.jsOpts.batchSize,
			jetstream.FetchMaxWait(b.jsOpts.fetchWait),
		)

		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}

			logger.Errorf("Fetch error for topic %s: %v", sub.topic, err)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}

		backoff = time.Second

		for msg := range msgs.Messages() {
			b.handleMessage(sub, msg)
		}

		if msgs.Error() != nil {
			logger.Errorf("Message batch error for topic %s: %v", sub.topic, msgs.Error())
		}
	}
}

func (b *jetStreamBroker) handleMessage(sub *jsSubscriber, msg jetstream.Msg) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("Panic in handler for topic %s: %v", sub.topic, r)
			if err := msg.Nak(); err != nil {
				logger.Errorf("Failed to nak message after panic: %v", err)
			}
		}
	}()

	event := &jsEvent{
		msg:   msg,
		topic: sub.topic,
	}

	err := sub.handler(event)

	if err != nil {
		event.err = err
		if nakErr := msg.Nak(); nakErr != nil {
			logger.Errorf("Failed to nak message: %v", nakErr)
		}
		return
	}

	if ackErr := msg.Ack(); ackErr != nil {
		logger.Errorf("Failed to ack message: %v", ackErr)
	}
}

func streamNameFromTopic(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) == 0 {
		return "DEFAULT"
	}

	return strings.ToUpper(strings.ReplaceAll(parts[0], "-", "_"))
}

func sanitizeTopicForDurable(topic string) string {
	s := strings.ReplaceAll(topic, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return strings.ToLower(s)
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
