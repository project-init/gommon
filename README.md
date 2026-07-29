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

## Useful Docs

- [Code of Conduct](./CODE_OF_CONDUCT.md)
- [Contribution Guide](./CONTRIBUTING.md)
