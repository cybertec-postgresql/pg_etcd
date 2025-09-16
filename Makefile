# pg_etcd Makefile

.PHONY: build test lint clean install help

# Build variables
BINARY_NAME=pg_etcd
DOCKER_IMAGE=cybertecpostgresql/$(BINARY_NAME)
VERSION?=dev
BUILD_DIR=.
LDFLAGS:=-X main.version=$(VERSION) -X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) -X main.commit=$(shell git rev-parse --short HEAD)

# Default target
.DEFAULT_GOAL := help

foo:
	@echo $(LDFLAGS)

## Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/pg_etcd

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
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/pg_etcd
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/pg_etcd
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/pg_etcd
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/pg_etcd
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/pg_etcd

## Build Docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(VERSION) --build-arg LDFLAGS="$(LDFLAGS)" .
	@docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest
	@echo "Docker image $(DOCKER_IMAGE):$(VERSION) built successfully."

## Push Docker image to registry
docker-push: docker-build
	@echo "Pushing Docker image to registry..."
	@docker push $(DOCKER_IMAGE):$(VERSION)
	@docker push $(DOCKER_IMAGE):latest
	@echo "Docker images pushed successfully."

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
