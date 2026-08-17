//go:build integration

package eventbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPublish_DeliversEventToRuleTarget(t *testing.T) {
	const (
		busName   = "gommon-events-dev"
		queueName = "gommon-events-dev"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	config, err := awsconfig.LoadDefaultConfig(ctx)
	require.NoError(t, err)

	sqsClient := sqs.NewFromConfig(config)

	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	require.NoError(t, err)
	queueURL := aws.ToString(queue.QueueUrl)

	client, err := New(busName, 5*time.Second, 3)
	require.NoError(t, err)

	eventID := uuid.NewString()
	detail := `{"order_id":"order-123","status":"created"}`
	err = client.Publish(ctx, eventID, "gommon.integration", "OrderCreated", detail)
	require.NoError(t, err)

	message, err := receiveMessage(ctx, sqsClient, queueURL)
	require.NoError(t, err)

	var delivered struct {
		Detail     json.RawMessage `json:"detail"`
		DetailType string          `json:"detail-type"`
		Source     string          `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(aws.ToString(message.Body)), &delivered))
	require.JSONEq(t, detail, string(delivered.Detail))
	require.Equal(t, "OrderCreated", delivered.DetailType)
	require.Equal(t, "gommon.integration", delivered.Source)
}

func receiveMessage(ctx context.Context, client *sqs.Client, queueURL string) (*sqstypes.Message, error) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		output, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     1,
		})
		if err != nil {
			return nil, err
		}
		if len(output.Messages) > 0 {
			return &output.Messages[0], nil
		}
	}

	return nil, fmt.Errorf("timed out waiting for EventBridge target message")
}
