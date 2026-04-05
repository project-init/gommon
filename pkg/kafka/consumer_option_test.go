package kafka

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConsumerOptions(t *testing.T) {
	t.Parallel()

	o := DefaultConsumerOptions()

	assert.Equal(t, 100, o.MaxPollRecords, "MaxPollRecords")
	assert.Equal(t, 45*time.Second, o.SessionTimeout, "SessionTimeout")
	assert.Equal(t, 10*time.Second, o.RevocationTimeout, "RevocationTimeout")
	assert.NotNil(t, o.Logger, "Logger")

	// Required fields are empty by default and should fail validation.
	require.Error(t, o.Validate())
}

func TestConsumerOptionsValidate_Success(t *testing.T) {
	t.Parallel()

	o := DefaultConsumerOptions()
	o.Brokers = []string{"localhost:9092"}
	o.GroupID = "test-group"
	o.Topics = []string{"test-topic"}

	require.NoError(t, o.Validate())
}

func TestConsumerOptionsValidate_Errors(t *testing.T) {
	t.Parallel()

	valid := func() ConsumerOptions {
		o := DefaultConsumerOptions()
		o.Brokers = []string{"localhost:9092"}
		o.GroupID = "test-group"
		o.Topics = []string{"test-topic"}
		return o
	}

	t.Run("missing Brokers", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.Brokers = nil

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.Brokers is required")
	})

	t.Run("empty Brokers", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.Brokers = []string{}

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.Brokers is required")
	})

	t.Run("missing GroupID", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.GroupID = ""

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.GroupID is required")
	})

	t.Run("missing Topics", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.Topics = nil

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.Topics is required")
	})

	t.Run("MaxPollRecords <= 0", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.MaxPollRecords = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.MaxPollRecords must be > 0")
	})

	t.Run("SessionTimeout <= 0", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.SessionTimeout = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.SessionTimeout must be > 0")
	})

	t.Run("RevocationTimeout <= 0", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.RevocationTimeout = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.RevocationTimeout must be > 0")
	})

	t.Run("RevocationTimeout >= SessionTimeout", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.RevocationTimeout = o.SessionTimeout

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.RevocationTimeout")
		assert.Contains(t, err.Error(), "must be less than SessionTimeout")

		o = valid()
		o.RevocationTimeout = o.SessionTimeout + time.Second

		err = o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be less than SessionTimeout")
	})
}

func TestConsumerFunctionalOptions_SetFields(t *testing.T) {
	t.Parallel()

	o := DefaultConsumerOptions()

	l := slog.New(slog.NewTextHandler(nil, nil))
	var client consumerClient = &fakeKafka{t: t}

	optFns := []ConsumerOption{
		WithConsumerBrokers([]string{"b1:9092", "b2:9092"}),
		WithConsumerGroupID("my-group"),
		WithConsumerTopics([]string{"topic-a", "topic-b"}),
		WithConsumerLogger(l),
		WithConsumerMaxPollRecords(50),
		WithConsumerSessionTimeout(30 * time.Second),
		WithConsumerRevocationTimeout(5 * time.Second),
		WithConsumerClient(client),
	}

	for _, fn := range optFns {
		require.NoError(t, fn(&o))
	}

	assert.Equal(t, []string{"b1:9092", "b2:9092"}, o.Brokers, "Brokers")
	assert.Equal(t, "my-group", o.GroupID, "GroupID")
	assert.Equal(t, []string{"topic-a", "topic-b"}, o.Topics, "Topics")
	assert.Equal(t, l, o.Logger, "Logger")
	assert.Equal(t, 50, o.MaxPollRecords, "MaxPollRecords")
	assert.Equal(t, 30*time.Second, o.SessionTimeout, "SessionTimeout")
	assert.Equal(t, 5*time.Second, o.RevocationTimeout, "RevocationTimeout")
	assert.Equal(t, client, o.Client, "Client")

	// Ensure final options validate.
	require.NoError(t, o.Validate())
}

func TestNewConsumer_NilHandler(t *testing.T) {
	t.Parallel()

	_, err := NewConsumer(nil, WithConsumerBrokers([]string{"localhost:9092"}), WithConsumerGroupID("g"), WithConsumerTopics([]string{"t"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler is required")
}

func TestNewConsumer_ValidationError(t *testing.T) {
	t.Parallel()

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {}

	// Missing required fields.
	_, err := NewConsumer(handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "options.Brokers is required")
}

func TestNewConsumer_NilOptionSkipped(t *testing.T) {
	t.Parallel()

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {}

	_, err := NewConsumer(
		handler,
		nil, // should be skipped
		WithConsumerBrokers([]string{"localhost:9092"}),
		WithConsumerGroupID("g"),
		WithConsumerTopics([]string{"t"}),
		WithConsumerClient(&fakeKafka{t: t}),
	)
	require.NoError(t, err)
}
