#!/bin/sh

set -eu

awslocal events create-event-bus --name gommon-events-dev
awslocal sqs create-queue --queue-name gommon-events-dev
