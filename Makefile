# etcd_fdw Makefile

.PHONY: build test lint clean install help

# Build variables
BINARY_NAME=etcd_fdw
VERSION?=dev
BUILD_DIR=build
LDFLAGS=-ldflags="-X main.version=$(VERSION)"

# Default target
.DEFAULT_GOAL := help

## Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/etcd_fdw

## Run tests
test:
	@echo "Running tests..."
	@go test -v -coverprofile=coverage.out ./...

## Run integration tests (requires PostgreSQL and etcd)
test-integration:
	@echo "Running integration tests..."
	@go test -v -tags=integration ./tests/...

## Run linting
lint:
	@echo "Running linter..."
	@golangci-lint run

## Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out

## Install binary to $GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

## Run all checks (test + lint)
check: test lint
	@echo "All checks passed!"

## Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/etcd_fdw
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/etcd_fdw
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/etcd_fdw
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/etcd_fdw
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/etcd_fdw

## Run the binary (requires PostgreSQL and etcd)
run: build
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME) --help

## Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

## Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	@go mod tidy

## Show help
help:
	@echo "Available commands:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
