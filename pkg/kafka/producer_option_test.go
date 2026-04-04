package kafka

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultProducerOptions(t *testing.T) {
	t.Parallel()

	o := DefaultProducerOptions()

	assert.Equal(t, 30*time.Second, o.DeliveryTimeout, "DeliveryTimeout")
	assert.Equal(t, 5, o.MaxRetries, "MaxRetries")
	assert.NotNil(t, o.Logger, "Logger")

	// Required fields are empty by default and should fail validation.
	require.Error(t, o.Validate())
}

func TestProducerOptionsValidate_Success(t *testing.T) {
	t.Parallel()

	o := DefaultProducerOptions()
	o.Brokers = []string{"localhost:9092"}

	require.NoError(t, o.Validate())
}

func TestProducerOptionsValidate_Errors(t *testing.T) {
	t.Parallel()

	valid := func() ProducerOptions {
		o := DefaultProducerOptions()
		o.Brokers = []string{"localhost:9092"}
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

	t.Run("DeliveryTimeout <= 0", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.DeliveryTimeout = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.DeliveryTimeout must be > 0")
	})

	t.Run("MaxRetries < 0", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.MaxRetries = -1

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.MaxRetries must be >= 0")
	})

	t.Run("MaxRetries = 0 is valid", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.MaxRetries = 0

		require.NoError(t, o.Validate())
	})
}

func TestProducerFunctionalOptions_SetFields(t *testing.T) {
	t.Parallel()

	o := DefaultProducerOptions()

	l := slog.New(slog.NewTextHandler(nil, nil))
	var client producerClient = &fakeProducer{}

	optFns := []ProducerOption{
		WithProducerBrokers([]string{"b1:9092", "b2:9092"}),
		WithProducerLogger(l),
		WithProducerDeliveryTimeout(10 * time.Second),
		WithProducerMaxRetries(3),
		WithProducerClient(client),
	}

	for _, fn := range optFns {
		require.NoError(t, fn(&o))
	}

	assert.Equal(t, []string{"b1:9092", "b2:9092"}, o.Brokers, "Brokers")
	assert.Equal(t, l, o.Logger, "Logger")
	assert.Equal(t, 10*time.Second, o.DeliveryTimeout, "DeliveryTimeout")
	assert.Equal(t, 3, o.MaxRetries, "MaxRetries")
	assert.Equal(t, client, o.Client, "Client")

	// Ensure final options validate.
	require.NoError(t, o.Validate())
}

func TestNewProducer_ValidationError(t *testing.T) {
	t.Parallel()

	// Missing required fields.
	_, err := NewProducer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "options.Brokers is required")
}

func TestNewProducer_NilOptionSkipped(t *testing.T) {
	t.Parallel()

	_, err := NewProducer(
		nil, // should be skipped
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(&fakeProducer{}),
	)
	require.NoError(t, err)
}
