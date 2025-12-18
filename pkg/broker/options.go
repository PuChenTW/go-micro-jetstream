package broker

import (
	"context"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go-micro.dev/v5/broker"
)

type jetStreamOptions struct {
	batchSize    int
	fetchWait    time.Duration
	clientName   string
	natsOpts     []nats.Option
	streamConfig *jetstream.StreamConfig
}

type optionsKey struct{}

func WithBatchSize(size int) broker.Option {
	return func(o *broker.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		jsOpts := getJetStreamOptions(o)
		jsOpts.batchSize = size
		o.Context = context.WithValue(o.Context, optionsKey{}, jsOpts)
	}
}

func WithFetchWait(duration time.Duration) broker.Option {
	return func(o *broker.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		jsOpts := getJetStreamOptions(o)
		jsOpts.fetchWait = duration
		o.Context = context.WithValue(o.Context, optionsKey{}, jsOpts)
	}
}

func WithClientName(name string) broker.Option {
	return func(o *broker.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		jsOpts := getJetStreamOptions(o)
		jsOpts.clientName = name
		o.Context = context.WithValue(o.Context, optionsKey{}, jsOpts)
	}
}

func WithNATSOptions(opts ...nats.Option) broker.Option {
	return func(o *broker.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		jsOpts := getJetStreamOptions(o)
		jsOpts.natsOpts = append(jsOpts.natsOpts, opts...)
		o.Context = context.WithValue(o.Context, optionsKey{}, jsOpts)
	}
}

func WithStreamConfig(cfg jetstream.StreamConfig) broker.Option {
	return func(o *broker.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		jsOpts := getJetStreamOptions(o)
		jsOpts.streamConfig = &cfg
		o.Context = context.WithValue(o.Context, optionsKey{}, jsOpts)
	}
}

func getJetStreamOptions(o *broker.Options) jetStreamOptions {
	if o.Context == nil {
		return defaultJetStreamOptions()
	}

	jsOpts, ok := o.Context.Value(optionsKey{}).(jetStreamOptions)
	if !ok {
		return defaultJetStreamOptions()
	}

	return jsOpts
}

func defaultJetStreamOptions() jetStreamOptions {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return jetStreamOptions{
		batchSize:  10,
		fetchWait:  5 * time.Second,
		clientName: "go-micro-" + hostname,
		natsOpts:   []nats.Option{},
	}
}
