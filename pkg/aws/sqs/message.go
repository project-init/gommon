package sqs

import "github.com/aws/aws-sdk-go-v2/service/sqs/types"

// Message wraps an SQS message. The handler calls Mark when it is done with the message; a message not marked
// is redelivered once its visibility timeout ends.
type Message struct {
	raw  types.Message
	mark func(types.Message)
}

// Raw returns the underlying AWS SDK message.
// This keeps the wrapper non-leaky while still allowing full access when needed.
func (m Message) Raw() types.Message { return m.raw }

// Mark best-effort enqueues this message for background deletion.
// It is a no-op if required fields are missing, once the message has been dropped, or after the handler has
// returned.
func (m Message) Mark() {
	if m.mark == nil {
		return
	}
	m.mark(m.raw)
}

// HandlerFunc is defined in worker.go.
