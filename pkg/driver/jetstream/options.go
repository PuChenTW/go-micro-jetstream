package jetstream

import (
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type Options struct {
	Addrs        []string
	BatchSize    int
	FetchWait    time.Duration
	ClientName   string
	NatsOptions  []nats.Option
	StreamConfig *jetstream.StreamConfig
	Logger       Logger
}

type Option func(*Options)

func defaultOptions() Options {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	return Options{
		Addrs:      []string{nats.DefaultURL},
		BatchSize:  10,
		FetchWait:  5 * time.Second,
		ClientName: "go-micro-" + hostname,
		Logger:     log.Default(),
	}
}

func WithAddrs(addrs ...string) Option {
	return func(o *Options) {
		o.Addrs = addrs
	}
}

func WithBatchSize(size int) Option {
	return func(o *Options) {
		o.BatchSize = size
	}
}

func WithFetchWait(wait time.Duration) Option {
	return func(o *Options) {
		o.FetchWait = wait
	}
}

func WithClientName(name string) Option {
	return func(o *Options) {
		o.ClientName = name
	}
}

func WithNatsOptions(opts ...nats.Option) Option {
	return func(o *Options) {
		o.NatsOptions = opts
	}
}

func WithStreamConfig(cfg jetstream.StreamConfig) Option {
	return func(o *Options) {
		o.StreamConfig = &cfg
	}
}

func WithLogger(l Logger) Option {
	return func(o *Options) {
		o.Logger = l
	}
}
