package broker

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/suite"
	"go-micro.dev/v5/broker"
)

// HelperFunctionsSuite tests all pure helper functions
type HelperFunctionsSuite struct {
	suite.Suite
}

func (s *HelperFunctionsSuite) TestStreamNameFromTopic() {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"with dots", "test.messages", "TEST"},
		{"with hyphens", "my-topic.sub", "MY_TOPIC"},
		{"single word", "single", "SINGLE"},
		{"multiple dots", "a.b.c.d", "A"},
		{"lowercase only", "lowercase", "LOWERCASE"},
		{"uppercase input", "UPPERCASE.test", "UPPERCASE"},
		{"mixed case", "MixedCase.Topic", "MIXEDCASE"},
		{"hyphen in first segment", "order-service.created", "ORDER_SERVICE"},
		{"complex name", "user-profile.update.v2", "USER_PROFILE"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := streamNameFromTopic(tt.input)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *HelperFunctionsSuite) TestMarshalMessage() {
	tests := []struct {
		name          string
		message       *broker.Message
		expectedBody  string
		expectedHeaders int
	}{
		{
			name: "body only",
			message: &broker.Message{
				Body: []byte("test body"),
			},
			expectedBody:  "test body",
			expectedHeaders: 0,
		},
		{
			name: "body with headers",
			message: &broker.Message{
				Header: map[string]string{"key1": "value1", "key2": "value2"},
				Body:   []byte("test body"),
			},
			expectedBody:  "test body",
			expectedHeaders: 2,
		},
		{
			name: "empty body",
			message: &broker.Message{
				Body: []byte{},
			},
			expectedBody:  "",
			expectedHeaders: 0,
		},
		{
			name: "nil body",
			message: &broker.Message{
				Header: map[string]string{"key": "value"},
			},
			expectedBody:  "",
			expectedHeaders: 1,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			body, headers := marshalMessage(tt.message)
			s.Equal(tt.expectedBody, string(body))
			s.Equal(tt.expectedHeaders, len(headers))

			if tt.message.Header != nil {
				for k, v := range tt.message.Header {
					s.Equal(v, headers.Get(k))
				}
			}
		})
	}
}

func TestHelperFunctionsSuite(t *testing.T) {
	if testing.Short() {
		suite.Run(t, new(HelperFunctionsSuite))
	}
}

// OptionsUnitSuite tests all option functions
type OptionsUnitSuite struct {
	suite.Suite
}

func (s *OptionsUnitSuite) TestWithBatchSize() {
	tests := []struct {
		name      string
		batchSize int
	}{
		{"default", 10},
		{"small", 1},
		{"medium", 50},
		{"large", 100},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			b := NewBroker(WithBatchSize(tt.batchSize))
			jsb := b.(*jetStreamBroker)
			s.Equal(tt.batchSize, jsb.jsOpts.batchSize)
		})
	}
}

func (s *OptionsUnitSuite) TestWithFetchWait() {
	tests := []struct {
		name      string
		fetchWait time.Duration
	}{
		{"1 second", 1 * time.Second},
		{"5 seconds", 5 * time.Second},
		{"30 seconds", 30 * time.Second},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			b := NewBroker(WithFetchWait(tt.fetchWait))
			jsb := b.(*jetStreamBroker)
			s.Equal(tt.fetchWait, jsb.jsOpts.fetchWait)
		})
	}
}

func (s *OptionsUnitSuite) TestWithClientName() {
	tests := []struct {
		name       string
		clientName string
	}{
		{"custom name", "test-client"},
		{"service name", "order-service"},
		{"with dash", "my-service-v2"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			b := NewBroker(WithClientName(tt.clientName))
			jsb := b.(*jetStreamBroker)
			s.Equal(tt.clientName, jsb.jsOpts.clientName)
		})
	}
}

func (s *OptionsUnitSuite) TestWithNATSOptions() {
	natsOpt := nats.Name("test-nats-client")
	b := NewBroker(WithNATSOptions(natsOpt))
	jsb := b.(*jetStreamBroker)

	s.NotNil(jsb.jsOpts.natsOpts)
	s.Len(jsb.jsOpts.natsOpts, 1)
}

func (s *OptionsUnitSuite) TestWithStreamConfig() {
	cfg := jetstream.StreamConfig{
		Retention: jetstream.LimitsPolicy,
		MaxAge:    24 * time.Hour,
	}

	b := NewBroker(WithStreamConfig(cfg))
	jsb := b.(*jetStreamBroker)

	s.NotNil(jsb.jsOpts.streamConfig)
	s.Equal(jetstream.LimitsPolicy, jsb.jsOpts.streamConfig.Retention)
	s.Equal(24*time.Hour, jsb.jsOpts.streamConfig.MaxAge)
}

func (s *OptionsUnitSuite) TestGetJetStreamOptions_Defaults() {
	opts := &broker.Options{}
	jsOpts := getJetStreamOptions(opts)

	s.Equal(10, jsOpts.batchSize)
	s.Equal(5*time.Second, jsOpts.fetchWait)
	s.Contains(jsOpts.clientName, "go-micro-")
	s.NotNil(jsOpts.natsOpts)
}

func (s *OptionsUnitSuite) TestGetJetStreamOptions_WithCustomValues() {
	opts := &broker.Options{}
	WithBatchSize(20)(opts)
	WithFetchWait(10 * time.Second)(opts)
	WithClientName("custom-client")(opts)

	jsOpts := getJetStreamOptions(opts)

	s.Equal(20, jsOpts.batchSize)
	s.Equal(10*time.Second, jsOpts.fetchWait)
	s.Equal("custom-client", jsOpts.clientName)
}

func TestOptionsUnitSuite(t *testing.T) {
	if testing.Short() {
		suite.Run(t, new(OptionsUnitSuite))
	}
}

// BrokerUnitSuite tests broker methods that don't require NATS
type BrokerUnitSuite struct {
	suite.Suite
}

func (s *BrokerUnitSuite) TestNewBroker_DefaultOptions() {
	b := NewBroker()

	s.NotNil(b)
	jsb := b.(*jetStreamBroker)
	s.NotNil(jsb.subs)
	s.False(jsb.connected)
	s.NotNil(jsb.opts)
}

func (s *BrokerUnitSuite) TestNewBroker_WithOptions() {
	b := NewBroker(
		broker.Addrs("localhost:4222"),
		WithBatchSize(20),
		WithFetchWait(10*time.Second),
		WithClientName("test-client"),
	)

	s.NotNil(b)
	jsb := b.(*jetStreamBroker)

	s.Equal(20, jsb.jsOpts.batchSize)
	s.Equal(10*time.Second, jsb.jsOpts.fetchWait)
	s.Equal("test-client", jsb.jsOpts.clientName)
	s.Contains(jsb.opts.Addrs, "localhost:4222")
}

func (s *BrokerUnitSuite) TestInit() {
	b := NewBroker()
	jsb := b.(*jetStreamBroker)

	s.Equal(10, jsb.jsOpts.batchSize)

	err := b.Init(WithBatchSize(30))
	s.NoError(err)
	s.Equal(30, jsb.jsOpts.batchSize)
}

func (s *BrokerUnitSuite) TestInit_MultipleOptions() {
	b := NewBroker()

	err := b.Init(
		broker.Addrs("nats://localhost:4223"),
		WithBatchSize(50),
		WithClientName("updated-client"),
	)

	s.NoError(err)
	jsb := b.(*jetStreamBroker)

	s.Equal(50, jsb.jsOpts.batchSize)
	s.Equal("updated-client", jsb.jsOpts.clientName)
	s.Contains(jsb.opts.Addrs, "nats://localhost:4223")
}

func (s *BrokerUnitSuite) TestOptions() {
	b := NewBroker(broker.Addrs("localhost:4222"))
	opts := b.Options()

	s.NotNil(opts)
	s.Contains(opts.Addrs, "localhost:4222")
}

func (s *BrokerUnitSuite) TestAddress_NotConnected() {
	b := NewBroker()
	addr := b.Address()

	s.Empty(addr)
}

func (s *BrokerUnitSuite) TestString() {
	b := NewBroker()
	s.Equal("jetstream", b.String())
}

func (s *BrokerUnitSuite) TestInternalState() {
	b := NewBroker()
	jsb := b.(*jetStreamBroker)

	s.NotNil(jsb.subs)
	s.Equal(0, len(jsb.subs))
	s.False(jsb.connected)
	s.Nil(jsb.nc)
	s.Nil(jsb.js)
}

func TestBrokerUnitSuite(t *testing.T) {
	if testing.Short() {
		suite.Run(t, new(BrokerUnitSuite))
	}
}

// ValidationSuite tests the durable name validation function
type ValidationSuite struct {
	suite.Suite
}

func (s *ValidationSuite) TestValidateDurableName_Valid() {
	tests := []struct {
		name        string
		durableName string
	}{
		{"alphanumeric only", "myqueue"},
		{"with hyphens", "my-queue-name"},
		{"with underscores", "my_queue_name"},
		{"mixed alphanumeric", "queue123"},
		{"uppercase", "MYQUEUE"},
		{"mixed case", "MyQueue"},
		{"single char", "q"},
		{"max recommended length", "a1234567890123456789012345678901"},
		{"with numbers and hyphens", "queue-v2-prod"},
		{"complex valid", "order_processor_v2-prod"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := validateDurableName(tt.durableName)
			s.NoError(err, "Expected '%s' to be valid", tt.durableName)
		})
	}
}

func (s *ValidationSuite) TestValidateDurableName_Invalid() {
	tests := []struct {
		name        string
		durableName string
		errorPart   string
	}{
		{"empty string", "", "cannot be empty"},
		{"with period", "my.queue", "cannot contain period"},
		{"with asterisk", "my*queue", "cannot contain asterisk"},
		{"with greater-than", "my>queue", "cannot contain greater-than"},
		{"with forward slash", "my/queue", "cannot contain path separators"},
		{"with backslash", "my\\queue", "cannot contain path separators"},
		{"with space", "my queue", "cannot contain whitespace"},
		{"with tab", "my\tqueue", "cannot contain whitespace"},
		{"with newline", "my\nqueue", "cannot contain whitespace"},
		{"with carriage return", "my\rqueue", "cannot contain whitespace"},
		{"leading space", " myqueue", "cannot contain whitespace"},
		{"trailing space", "myqueue ", "cannot contain whitespace"},
		{"control character", "my\x00queue", "cannot contain whitespace or non-printable"},
		{"DEL character", "my\x7Fqueue", "cannot contain whitespace or non-printable"},
		{"too long", strings.Repeat("a", 256), "cannot exceed 255 characters"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := validateDurableName(tt.durableName)
			s.Error(err, "Expected '%s' to be invalid", tt.durableName)
			if err != nil {
				s.Contains(err.Error(), tt.errorPart)
			}
		})
	}
}

func (s *ValidationSuite) TestValidateDurableName_PositionReporting() {
	tests := []struct {
		name        string
		durableName string
		expectedPos int
	}{
		{"period at start", ".queue", 0},
		{"period in middle", "my.queue", 2},
		{"asterisk at end", "queue*", 5},
		{"slash in middle", "my/queue", 2},
		{"space at position 3", "foo bar", 3},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := validateDurableName(tt.durableName)
			s.Error(err)
			s.Contains(err.Error(), fmt.Sprintf("position %d", tt.expectedPos))
		})
	}
}

func TestValidationSuite(t *testing.T) {
	if testing.Short() {
		suite.Run(t, new(ValidationSuite))
	}
}
