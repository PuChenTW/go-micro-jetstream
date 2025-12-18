package broker

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go-micro.dev/v5/broker"
)

type jsEvent struct {
	msg   jetstream.Msg
	topic string
	err   error
}

func (e *jsEvent) Topic() string {
	return e.topic
}

func (e *jsEvent) Message() *broker.Message {
	return &broker.Message{
		Header: extractHeaders(e.msg),
		Body:   e.msg.Data(),
	}
}

func (e *jsEvent) Ack() error {
	return e.msg.Ack()
}

func (e *jsEvent) Error() error {
	return e.err
}

type jsSubscriber struct {
	id       string
	topic    string
	queue    string
	handler  broker.Handler
	consumer jetstream.Consumer
	cancel   context.CancelFunc
	opts     broker.SubscribeOptions
}

func (s *jsSubscriber) Options() broker.SubscribeOptions {
	return s.opts
}

func (s *jsSubscriber) Topic() string {
	return s.topic
}

func (s *jsSubscriber) Unsubscribe() error {
	s.cancel()
	return nil
}

func extractHeaders(msg jetstream.Msg) map[string]string {
	headers := make(map[string]string)

	natsHeaders := msg.Headers()
	if natsHeaders == nil {
		return headers
	}

	for key, values := range natsHeaders {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	return headers
}

func marshalMessage(msg *broker.Message) ([]byte, nats.Header) {
	var headers nats.Header

	if len(msg.Header) > 0 {
		headers = make(nats.Header)
		for key, value := range msg.Header {
			headers.Set(key, value)
		}
	}

	return msg.Body, headers
}
