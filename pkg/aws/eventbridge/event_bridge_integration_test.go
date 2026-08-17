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
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
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

	eventBridgeClient := eventbridge.NewFromConfig(config)
	sqsClient := sqs.NewFromConfig(config)

	suffix := uuid.NewString()
	ruleName := "gommon-rule-" + suffix

	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	require.NoError(t, err)
	queueURL := aws.ToString(queue.QueueUrl)

	queueAttributes, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)
	queueARN := queueAttributes.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

	eventPattern, err := json.Marshal(map[string]any{
		"source": []string{"gommon.integration"},
	})
	require.NoError(t, err)

	_, err = eventBridgeClient.PutRule(ctx, &eventbridge.PutRuleInput{
		EventBusName: aws.String(busName),
		Name:         aws.String(ruleName),
		EventPattern: aws.String(string(eventPattern)),
		State:        eventbridgetypes.RuleStateEnabled,
	})
	require.NoError(t, err)

	_, err = eventBridgeClient.PutTargets(ctx, &eventbridge.PutTargetsInput{
		EventBusName: aws.String(busName),
		Rule:         aws.String(ruleName),
		Targets: []eventbridgetypes.Target{{
			Arn: aws.String(queueARN),
			Id:  aws.String("sqs-target"),
		}},
	})
	require.NoError(t, err)

	client, err := New(busName, 5*time.Second, 3)
	require.NoError(t, err)

	eventID := uuid.NewString()
	detail := `{"order_id":"order-123","status":"created"}`
	err = client.Publish(ctx, eventID, "gommon.integration", "OrderCreated", detail)
	require.NoError(t, err)

	message, err := receiveMessage(ctx, sqsClient, queueURL)
	require.NoError(t, err)

	var delivered struct {
		Detail     string `json:"detail"`
		DetailType string `json:"detail-type"`
		Source     string `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(aws.ToString(message.Body)), &delivered))
	require.Equal(t, detail, delivered.Detail)
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
