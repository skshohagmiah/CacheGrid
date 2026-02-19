# Contributing to CacheGrid

Thank you for your interest in contributing to CacheGrid!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/cachegrid.git`
3. Create a branch: `git checkout -b my-feature`
4. Make your changes
5. Run tests: `go test ./... -v`
6. Push and open a Pull Request

## Development

### Prerequisites

- Go 1.22 or later

### Build

```bash
go build ./...
```

### Test

```bash
go test ./... -v -count=1
```

### Benchmarks

```bash
go test -bench=. -benchmem
```

### Lint

```bash
go vet ./...
```

## Guidelines

- Write tests for new features
- Keep backward compatibility — existing tests must pass
- Follow existing code style and patterns
- Use `internal/` for implementation details; public API lives at the package root
- Keep the public API surface small and focused
- Avoid adding external dependencies unless absolutely necessary

## Pull Request Process

1. Ensure `go build ./...`, `go test ./...`, and `go vet ./...` pass
2. Update documentation if the public API changes
3. Add tests for new functionality
4. Keep commits focused — one logical change per commit

## Reporting Issues

- Use [GitHub Issues](https://github.com/skshohagmiah/cachegrid/issues)
- Include Go version, OS, and steps to reproduce
- For bugs, include a minimal reproduction if possible

## Code of Conduct

Be respectful, constructive, and collaborative. We're all here to build something useful.
