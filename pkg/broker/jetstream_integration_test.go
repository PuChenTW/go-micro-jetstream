package broker

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go-micro.dev/v5/broker"
)

// BrokerIntegrationSuite tests connection lifecycle
type BrokerIntegrationSuite struct {
	suite.Suite
	broker broker.Broker
}

func (s *BrokerIntegrationSuite) SetupSuite() {
	s.broker = testBrokerWithOptions(WithClientName("integration-test"))
	err := connectWithRetry(s.T(), s.broker, 3, 2*time.Second)
	s.Require().NoError(err, "NATS must be running on localhost:4222")
}

func (s *BrokerIntegrationSuite) TearDownSuite() {
	if s.broker != nil {
		s.broker.Disconnect()
	}
}

func (s *BrokerIntegrationSuite) TestConnect_Success() {
	b := testBrokerWithOptions()
	err := b.Connect()
	s.NoError(err)

	addr := b.Address()
	s.Contains(addr, "localhost:4222")

	b.Disconnect()
}

func (s *BrokerIntegrationSuite) TestConnect_Idempotent() {
	err := s.broker.Connect()
	s.NoError(err, "Second Connect should succeed")
}

func (s *BrokerIntegrationSuite) TestDisconnect_Idempotent() {
	b := testBrokerWithOptions()
	err := b.Connect()
	s.NoError(err)

	err = b.Disconnect()
	s.NoError(err)

	err = b.Disconnect()
	s.NoError(err, "Second Disconnect should succeed")
}

func (s *BrokerIntegrationSuite) TestAddress_AfterConnect() {
	addr := s.broker.Address()
	s.NotEmpty(addr)
	s.Contains(addr, "localhost:4222")
}

func (s *BrokerIntegrationSuite) TestPublish_BeforeConnect() {
	b := NewBroker(broker.Addrs("localhost:4222"))
	msg := &broker.Message{Body: []byte("test")}

	err := b.Publish("test.topic", msg)
	s.Error(err, "Should error when not connected")
	s.Contains(err.Error(), "not connected")
}

func (s *BrokerIntegrationSuite) TestSubscribe_BeforeConnect() {
	b := NewBroker(broker.Addrs("localhost:4222"))
	handler := successHandler()

	_, err := b.Subscribe("test.topic", handler)
	s.Error(err, "Should error when not connected")
	s.Contains(err.Error(), "not connected")
}

func TestBrokerIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(BrokerIntegrationSuite))
}

// PublishSubscribeIntegrationSuite tests pub/sub functionality
type PublishSubscribeIntegrationSuite struct {
	suite.Suite
	broker broker.Broker
	js     *jetStreamBroker
}

func (s *PublishSubscribeIntegrationSuite) SetupSuite() {
	s.broker = testBrokerWithOptions(WithClientName("pubsub-test"))
	err := connectWithRetry(s.T(), s.broker, 3, 2*time.Second)
	s.Require().NoError(err, "NATS must be running on localhost:4222")

	s.js = s.broker.(*jetStreamBroker)
}

func (s *PublishSubscribeIntegrationSuite) TearDownSuite() {
	if s.broker != nil {
		s.broker.Disconnect()
	}

	js, nc := getNATSJetStream(s.T())
	defer nc.Close()
	cleanupStreams(s.T(), js)
}

func (s *PublishSubscribeIntegrationSuite) TestSingleMessage() {
	topic := "test.single.message"
	messages := make(chan *broker.Message, 1)

	sub, err := s.broker.Subscribe(topic, messageCollector(messages), broker.Queue("single-msg-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	expectedBody := "test message body"
	err = publishTestMessage(s.T(), s.broker, topic, []byte(expectedBody), nil)
	s.NoError(err)

	received := waitForMessages(s.T(), messages, 1, 5*time.Second)
	s.Len(received, 1)
	assertMessageBody(s.T(), received[0], expectedBody)
}

func (s *PublishSubscribeIntegrationSuite) TestMultipleMessages() {
	topic := "test.multiple.messages"
	messages := make(chan *broker.Message, 5)

	sub, err := s.broker.Subscribe(topic, messageCollector(messages), broker.Queue("multi-msg-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	for i := 1; i <= 5; i++ {
		body := fmt.Sprintf("message %d", i)
		err = publishTestMessage(s.T(), s.broker, topic, []byte(body), nil)
		s.NoError(err)
	}

	received := waitForMessages(s.T(), messages, 5, 10*time.Second)
	s.Len(received, 5)

	for i := 0; i < 5; i++ {
		expected := fmt.Sprintf("message %d", i+1)
		assertMessageBody(s.T(), received[i], expected)
	}
}

func (s *PublishSubscribeIntegrationSuite) TestMultipleSubscribers() {
	// Use different topics to avoid WorkQueue filter conflicts
	topic1 := "broadcast.subscriber1"
	topic2 := "broadcast.subscriber2"
	messages1 := make(chan *broker.Message, 2)
	messages2 := make(chan *broker.Message, 2)

	sub1, err := s.broker.Subscribe(topic1, messageCollector(messages1), broker.Queue("subscriber-1"))
	s.Require().NoError(err)
	defer sub1.Unsubscribe()

	sub2, err := s.broker.Subscribe(topic2, messageCollector(messages2), broker.Queue("subscriber-2"))
	s.Require().NoError(err)
	defer sub2.Unsubscribe()

	time.Sleep(200 * time.Millisecond)

	// Publish to both topics
	body1 := "message for subscriber 1"
	err = publishTestMessage(s.T(), s.broker, topic1, []byte(body1), nil)
	s.NoError(err)

	body2 := "message for subscriber 2"
	err = publishTestMessage(s.T(), s.broker, topic2, []byte(body2), nil)
	s.NoError(err)

	received1 := waitForMessages(s.T(), messages1, 1, 5*time.Second)
	received2 := waitForMessages(s.T(), messages2, 1, 5*time.Second)

	s.Len(received1, 1)
	s.Len(received2, 1)
	assertMessageBody(s.T(), received1[0], body1)
	assertMessageBody(s.T(), received2[0], body2)
}

func (s *PublishSubscribeIntegrationSuite) TestQueueConsumption() {
	topic := "test.queue.consumption"
	var count1, count2 int64

	handler1, counter1 := countingHandler()
	handler2, counter2 := countingHandler()

	sub1, err := s.broker.Subscribe(topic, handler1, broker.Queue("test-queue"))
	s.Require().NoError(err)
	defer sub1.Unsubscribe()

	sub2, err := s.broker.Subscribe(topic, handler2, broker.Queue("test-queue"))
	s.Require().NoError(err)
	defer sub2.Unsubscribe()

	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 10; i++ {
		body := fmt.Sprintf("message %d", i)
		err = publishTestMessage(s.T(), s.broker, topic, []byte(body), nil)
		s.NoError(err)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(2 * time.Second)

	count1 = atomic.LoadInt64(counter1)
	count2 = atomic.LoadInt64(counter2)

	s.Greater(count1+count2, int64(0), "At least one subscriber should receive messages")
	s.Less(count1+count2, int64(20), "Messages should not be duplicated to both")
}

func (s *PublishSubscribeIntegrationSuite) TestHeadersPreserved() {
	topic := "test.headers.preserved"
	messages := make(chan *broker.Message, 1)

	sub, err := s.broker.Subscribe(topic, messageCollector(messages), broker.Queue("headers-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	headers := map[string]string{
		"Content-Type": "application/json",
		"X-Request-ID": "12345",
	}
	err = publishTestMessage(s.T(), s.broker, topic, []byte("test"), headers)
	s.NoError(err)

	received := waitForMessages(s.T(), messages, 1, 5*time.Second)
	s.Len(received, 1)

	assertMessageHeader(s.T(), received[0], "Content-Type", "application/json")
	assertMessageHeader(s.T(), received[0], "X-Request-ID", "12345")
}

func (s *PublishSubscribeIntegrationSuite) TestHandlerReturnsError() {
	topic := "test.handler.error"
	var attempts int64
	handler := func(e broker.Event) error {
		atomic.AddInt64(&attempts, 1)
		return fmt.Errorf("handler error")
	}

	sub, err := s.broker.Subscribe(topic, handler, broker.Queue("error-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	err = publishTestMessage(s.T(), s.broker, topic, []byte("test"), nil)
	s.NoError(err)

	time.Sleep(2 * time.Second)

	count := atomic.LoadInt64(&attempts)
	s.Greater(count, int64(1), "Message should be redelivered after NAK")
}

func (s *PublishSubscribeIntegrationSuite) TestHandlerPanics() {
	topic := "test.handler.panic"
	var attempts int64
	handler := func(e broker.Event) error {
		count := atomic.AddInt64(&attempts, 1)
		if count == 1 {
			panic("test panic")
		}
		return nil
	}

	sub, err := s.broker.Subscribe(topic, handler, broker.Queue("panic-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	err = publishTestMessage(s.T(), s.broker, topic, []byte("test"), nil)
	s.NoError(err)

	time.Sleep(2 * time.Second)

	count := atomic.LoadInt64(&attempts)
	s.GreaterOrEqual(count, int64(2), "Message should be redelivered after panic")
}

func (s *PublishSubscribeIntegrationSuite) TestStreamAutoCreation() {
	topic := "autocreate.test.topic"

	err := publishTestMessage(s.T(), s.broker, topic, []byte("test"), nil)
	s.NoError(err, "Stream should be auto-created")

	streamName := streamNameFromTopic(topic)
	s.Equal("AUTOCREATE", streamName)

	stream, err := s.js.js.Stream(s.js.opts.Context, streamName)
	s.NoError(err)
	s.Equal(streamName, stream.CachedInfo().Config.Name)
}

func TestPublishSubscribeIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(PublishSubscribeIntegrationSuite))
}

// ConcurrencyIntegrationSuite tests concurrent operations
type ConcurrencyIntegrationSuite struct {
	suite.Suite
	broker broker.Broker
}

func (s *ConcurrencyIntegrationSuite) SetupSuite() {
	s.broker = testBrokerWithOptions(WithClientName("concurrency-test"))
	err := connectWithRetry(s.T(), s.broker, 3, 2*time.Second)
	s.Require().NoError(err, "NATS must be running on localhost:4222")
}

func (s *ConcurrencyIntegrationSuite) TearDownSuite() {
	if s.broker != nil {
		s.broker.Disconnect()
	}

	js, nc := getNATSJetStream(s.T())
	defer nc.Close()
	cleanupStreams(s.T(), js)
}

func (s *ConcurrencyIntegrationSuite) TestConcurrentPublish() {
	topic := "test.concurrent.publish"
	const count = 10

	var wg sync.WaitGroup
	errors := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body := fmt.Sprintf("message %d", n)
			err := publishTestMessage(s.T(), s.broker, topic, []byte(body), nil)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		s.NoError(err)
	}
}

func (s *ConcurrencyIntegrationSuite) TestConcurrentSubscribe() {
	const count = 5

	var wg sync.WaitGroup
	errors := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Use unique topic for each subscriber to avoid WorkQueue conflicts
			topic := fmt.Sprintf("concurrent.subscribe.%d", n)
			queueName := fmt.Sprintf("concurrent-sub-%d", n)
			sub, err := s.broker.Subscribe(topic, successHandler(), broker.Queue(queueName))
			if err != nil {
				errors <- err
				return
			}
			time.Sleep(100 * time.Millisecond)
			sub.Unsubscribe()
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		s.NoError(err)
	}
}

func (s *ConcurrencyIntegrationSuite) TestMixedOperations() {
	topic := "test.mixed.operations"
	messages := make(chan *broker.Message, 50)

	sub, err := s.broker.Subscribe(topic, messageCollector(messages), broker.Queue("mixed-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(200 * time.Millisecond)

	var wg sync.WaitGroup
	const publishers = 5
	const messagesPerPublisher = 10

	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(publisherID int) {
			defer wg.Done()
			for j := 0; j < messagesPerPublisher; j++ {
				body := fmt.Sprintf("publisher-%d message-%d", publisherID, j)
				publishTestMessage(s.T(), s.broker, topic, []byte(body), nil)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	time.Sleep(2 * time.Second)

	receivedCount := len(messages)
	s.GreaterOrEqual(receivedCount, publishers*messagesPerPublisher)
}

func TestConcurrencyIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ConcurrencyIntegrationSuite))
}

// ErrorScenariosSuite tests error handling
type ErrorScenariosSuite struct {
	suite.Suite
	broker broker.Broker
}

func (s *ErrorScenariosSuite) SetupSuite() {
	s.broker = testBrokerWithOptions(WithClientName("error-test"))
	err := connectWithRetry(s.T(), s.broker, 3, 2*time.Second)
	s.Require().NoError(err, "NATS must be running on localhost:4222")
}

func (s *ErrorScenariosSuite) TearDownSuite() {
	if s.broker != nil {
		s.broker.Disconnect()
	}

	js, nc := getNATSJetStream(s.T())
	defer nc.Close()
	cleanupStreams(s.T(), js)
}

func (s *ErrorScenariosSuite) TestConnectionFailure() {
	b := NewBroker(broker.Addrs("localhost:9999"))
	err := b.Connect()
	s.Error(err, "Should fail to connect to invalid port")
}

func (s *ErrorScenariosSuite) TestPublishToInvalidBroker() {
	b := NewBroker(broker.Addrs("localhost:4222"))

	msg := &broker.Message{Body: []byte("test")}
	err := b.Publish("test.topic", msg)
	s.Error(err)
	s.Contains(err.Error(), "not connected")
}

func (s *ErrorScenariosSuite) TestInvalidDurableNames() {
	topic := "test.invalid.durable"

	tests := []struct {
		name        string
		durableName string
		errorPart   string
	}{
		{"empty queue name", "", "required"},
		{"with period", "my.queue", "invalid queue name"},
		{"with asterisk", "my*queue", "invalid queue name"},
		{"with space", "my queue", "invalid queue name"},
		{"with slash", "my/queue", "invalid queue name"},
		{"with backslash", "my\\queue", "invalid queue name"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.broker.Subscribe(
				topic,
				successHandler(),
				broker.Queue(tt.durableName),
			)
			s.Error(err, "Should reject invalid durable name: %s", tt.durableName)
			s.Contains(err.Error(), tt.errorPart)
		})
	}
}

func (s *ErrorScenariosSuite) TestSubscribeToInvalidBroker() {
	b := NewBroker(broker.Addrs("localhost:4222"))

	_, err := b.Subscribe("test.topic", successHandler())
	s.Error(err)
	s.Contains(err.Error(), "not connected")
}

func (s *ErrorScenariosSuite) TestUnsubscribe() {
	topic := "test.unsubscribe"
	var receivedCount int64

	handler := func(e broker.Event) error {
		atomic.AddInt64(&receivedCount, 1)
		return nil
	}

	sub, err := s.broker.Subscribe(topic, handler, broker.Queue("unsub-queue"))
	s.Require().NoError(err)

	time.Sleep(200 * time.Millisecond)

	// Publish message
	err = publishTestMessage(s.T(), s.broker, topic, []byte("test message"), nil)
	s.NoError(err)

	time.Sleep(1 * time.Second)

	count := atomic.LoadInt64(&receivedCount)
	s.GreaterOrEqual(count, int64(1), "Should receive at least one message")

	// Unsubscribe should complete without error
	err = sub.Unsubscribe()
	s.NoError(err, "Unsubscribe should succeed")

	// Additional unsubscribe should be idempotent
	err = sub.Unsubscribe()
	s.NoError(err, "Second unsubscribe should also succeed")
}

func (s *ErrorScenariosSuite) TestHandlerWithSequentialErrors() {
	topic := "test.sequential.errors"
	handler, counter := sequentialHandler(2, "intentional error")

	sub, err := s.broker.Subscribe(topic, handler, broker.Queue("seq-error-queue"))
	s.Require().NoError(err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	err = publishTestMessage(s.T(), s.broker, topic, []byte("test"), nil)
	s.NoError(err)

	time.Sleep(3 * time.Second)

	count := atomic.LoadInt64(counter)
	s.GreaterOrEqual(count, int64(3), "Should retry after errors and eventually succeed")
}

func TestErrorScenariosSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ErrorScenariosSuite))
}
