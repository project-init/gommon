#!/bin/sh

set -eu
set -x

region=us-east-1
event_bus_name=gommon-events-dev
queue_name=gommon-events-dev
rule_name=gommon-events-dev-rule

awslocal events create-event-bus \
  --region "$region" \
  --name "$event_bus_name"
awslocal sqs create-queue \
  --region "$region" \
  --queue-name "$queue_name"

queue_url=$(awslocal sqs get-queue-url \
  --region "$region" \
  --queue-name "$queue_name" \
  --query QueueUrl \
  --output text)
queue_arn=$(awslocal sqs get-queue-attributes \
  --region "$region" \
  --queue-url "$queue_url" \
  --attribute-names QueueArn \
  --query 'Attributes.QueueArn' \
  --output text)

awslocal events put-rule \
  --region "$region" \
  --event-bus-name "$event_bus_name" \
  --name "$rule_name" \
  --event-pattern '{"source":["gommon.integration"]}' \
  --state ENABLED
awslocal events put-targets \
  --region "$region" \
  --event-bus-name "$event_bus_name" \
  --rule "$rule_name" \
  --targets "[{\"Id\":\"sqs-target\",\"Arn\":\"$queue_arn\"}]"

awslocal events describe-event-bus \
  --region "$region" \
  --name "$event_bus_name" >/dev/null
awslocal sqs get-queue-url \
  --region "$region" \
  --queue-name "$queue_name" >/dev/null
