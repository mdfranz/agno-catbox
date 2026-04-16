# Project variables
BINARY_NAME=skill-runner
CMD_DIR=./cmd/skill-runner
PKG=github.com/mdfranz/skill-runner

# Build configuration
GO=go
GOFLAGS=-v
LDFLAGS=-ldflags="-s -w"

.PHONY: all build test clean lint run help

all: build

## build: Build the binary
build:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME) $(CMD_DIR)

## test: Run tests
test:
	$(GO) test -v ./...

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -rf dist/

## lint: Run golangci-lint (if installed)
lint:
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## run: Build and show help for the binary
run: build
	./$(BINARY_NAME) -help

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' Makefile | column -t -s ':' |  sed -e 's/^/ /'
