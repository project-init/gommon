# Gommon

Common Go Functionality

## Quick Start

1. `mise unit-test`

## Error Packages

Use `pkg/errors` for shared error values. Transport-specific mappings are available from:

- `pkg/errors/connect`
- `pkg/errors/grpc`
- `pkg/errors/http`

Import the transport package directly instead of calling transport mapping functions from `pkg/errors`.

## SQS Worker

`pkg/aws/sqs` runs a handler over an SQS queue. The handler calls `Mark` to delete a message it is done with; a message
not marked is redelivered when its visibility timeout ends.

- `WithWorkerCount(n)` runs `n` handlers at once. Each takes up to `BatchSize` of the oldest waiting messages, without
  waiting for a full batch.
- `WithMaxPrefetched(n)` lets up to `n` received messages wait in memory for a busy handler. The default is `10`. Use
  `0` for long jobs, so the worker receives only for idle handlers and waiting messages stay in the queue for other
  workers.
- The worker keeps every message it holds hidden, waiting or running, until the handler returns. Each receive hides
  messages for 6 minutes, and every 3 minutes the worker resets them to 6 minutes from then. To change both, use
  `WithHeartbeat(interval, extension)`: longer values make fewer SQS calls, but a crashed worker's messages take longer
  to reappear.
- If a message's visibility cannot be kept, the worker drops it and cancels its handler's context with the cause
  `ErrVisibilityLost`. SQS keeps a message hidden for at most 12 hours after it was received, so a longer job loses its
  message.
- On shutdown, waiting messages are not started and reappear when their visibility ends. Running handlers see their
  context cancelled, and their messages stay hidden until they return.

## Useful Docs

- [Code of Conduct](./CODE_OF_CONDUCT.md)
- [Contribution Guide](./CONTRIBUTING.md)
