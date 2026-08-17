package eventbridge

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	gaws "github.com/project-init/gommon/pkg/aws"
	"github.com/project-init/gommon/pkg/errors"
)

// PutEvents limits each complete entry to 256 KiB. Keep room for the entry's
// source, detail type, time, resources, and other EventBridge wrapper fields.
const (
	maxDetailSizeBytes      = 256*1024 - 10*1024
	defaultPutEventsTimeout = 5 * time.Second
	defaultMaxRetryAttempts = 3
)

type EventBridge struct {
	client           *eventbridge.Client
	timeout          time.Duration
	eventBusName     string
	maxRetryAttempts int
}

func New(eventBusName string, timeout time.Duration, maxRetryAttempts int) (*EventBridge, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout must be greater than or equal to 0", errors.ErrBadRequest)
	}
	if maxRetryAttempts < 1 {
		return nil, fmt.Errorf("%w: maxRetryAttempts must be greater than 0", errors.ErrBadRequest)
	}

	client := eventbridge.NewFromConfig(gaws.GetConfig(), func(opts *eventbridge.Options) {
		opts.Retryer = retry.NewStandard(func(standardOptions *retry.StandardOptions) {
			standardOptions.MaxAttempts = maxRetryAttempts
		})
	})

	return &EventBridge{
		timeout:          timeout,
		maxRetryAttempts: maxRetryAttempts,
		eventBusName:     eventBusName,
		client:           client,
	}, nil
}

func Default(eventBusName string) *EventBridge {
	eventBridge, _ := New(eventBusName, defaultPutEventsTimeout, defaultMaxRetryAttempts)
	return eventBridge
}

func (p *EventBridge) Publish(ctx context.Context, eventID, source, detailType, event string) error {
	if len(event) > maxDetailSizeBytes {
		err := fmt.Errorf(
			"%w: (event too large) event_id=%s size=%d max=%d",
			errors.ErrBadRequest,
			eventID,
			len(event),
			maxDetailSizeBytes,
		)
		return err
	}

	requestContext, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	output, err := p.client.PutEvents(requestContext, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{{
			EventBusName: aws.String(p.eventBusName),
			Source:       aws.String(source),
			DetailType:   aws.String(detailType),
			Detail:       aws.String(event),
		}},
	})

	if err != nil {
		return fmt.Errorf("%w: failed to publish event (%s) due to error", err, eventID)
	}

	if output == nil {
		return fmt.Errorf("failed to publish event (%s), output is nil", eventID)
	}

	if output.FailedEntryCount > 0 {
		var errorCode string
		var errorMessage string
		if len(output.Entries) > 0 {
			errorCode = aws.ToString(output.Entries[0].ErrorCode)
			errorMessage = aws.ToString(output.Entries[0].ErrorMessage)
		}

		return fmt.Errorf("failed to publish event (%s), code=%s, message=%s", eventID, errorCode, errorMessage)
	}

	return nil
}
