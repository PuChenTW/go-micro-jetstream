package broker

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"go-micro.dev/v5/broker"
)

// errorHandler returns a handler that always returns an error
func errorHandler(errMsg string) broker.Handler {
	return func(e broker.Event) error {
		return errors.New(errMsg)
	}
}

// panicHandler returns a handler that panics
func panicHandler(panicMsg string) broker.Handler {
	return func(e broker.Event) error {
		panic(panicMsg)
	}
}

// successHandler returns a handler that always succeeds
func successHandler() broker.Handler {
	return func(e broker.Event) error {
		return nil
	}
}

// countingHandler returns a handler that counts invocations
func countingHandler() (broker.Handler, *int64) {
	var count int64
	handler := func(e broker.Event) error {
		atomic.AddInt64(&count, 1)
		return nil
	}
	return handler, &count
}

// messageCollector returns a handler that collects received messages
func messageCollector(messages chan *broker.Message) broker.Handler {
	return func(e broker.Event) error {
		msg := e.Message()
		messages <- msg
		return nil
	}
}

// delayedHandler returns a handler with configurable delay
func delayedHandler(delay time.Duration) broker.Handler {
	return func(e broker.Event) error {
		time.Sleep(delay)
		return nil
	}
}

// conditionalErrorHandler returns error only for specific message content
func conditionalErrorHandler(errorCondition func(*broker.Message) bool, errMsg string) broker.Handler {
	return func(e broker.Event) error {
		msg := e.Message()
		if errorCondition(msg) {
			return fmt.Errorf("%s", errMsg)
		}
		return nil
	}
}

// sequentialHandler returns handler that succeeds after N failures
func sequentialHandler(failCount int, errMsg string) (broker.Handler, *int64) {
	var count int64
	handler := func(e broker.Event) error {
		current := atomic.AddInt64(&count, 1)
		if int(current) <= failCount {
			return fmt.Errorf("%s", errMsg)
		}
		return nil
	}
	return handler, &count
}

// blockingHandler returns handler that blocks until signal is sent
func blockingHandler(signal chan struct{}) broker.Handler {
	return func(e broker.Event) error {
		<-signal
		return nil
	}
}
