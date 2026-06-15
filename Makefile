# Prima Distributed Local - Makefile
BINARY_NAME := prima-installer
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X 'github.com/AR-Davis/prima_distributed_local/cmd.Version=$(VERSION)' -X 'github.com/AR-Davis/prima_distributed_local/build.Date=$(BUILD_DATE)'"

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Directories
DIST_DIR := dist
CMD_DIR := cmd/prima-installer

# Build for current platform
.PHONY: build
build:
	@echo "🔨 Building $(BINARY_NAME) v$(VERSION)..."
	mkdir -p $(DIST_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "✅ Build complete: $(DIST_DIR)/$(BINARY_NAME)"
	@ls -lh $(DIST_DIR)/$(BINARY_NAME)

# Build all platforms
.PHONY: build-all
build-all: build-linux build-darwin build-windows
	@echo "✅ All builds complete"

# Linux builds
.PHONY: build-linux
build-linux: build-linux-amd64 build-linux-arm64

.PHONY: build-linux-amd64
build-linux-amd64:
	@echo "🐧 Building for Linux amd64..."
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)

.PHONY: build-linux-arm64
build-linux-arm64:
	@echo "🐧 Building for Linux arm64..."
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)

# macOS builds
.PHONY: build-darwin
build-darwin: build-darwin-amd64 build-darwin-arm64

.PHONY: build-darwin-amd64
build-darwin-amd64:
	@echo "🍎 Building for macOS amd64..."
	mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./$(CMD_DIR)

.PHONY: build-darwin-arm64
build-darwin-arm64:
	@echo "🍎 Building for macOS arm64..."
	mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)

# Windows builds
.PHONY: build-windows
build-windows:
	@echo "🪟 Building for Windows amd64..."
	mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)

# Test
.PHONY: test
test:
	@echo "🧪 Running tests..."
	$(GOTEST) -v ./...

# Test with coverage
.PHONY: test-coverage
test-coverage:
	@echo "📊 Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

# Clean
.PHONY: clean
clean:
	@echo "🧹 Cleaning..."
	$(GOCLEAN)
	rm -rf $(DIST_DIR)
	rm -f coverage.out coverage.html
	@echo "✅ Clean complete"

# Download dependencies
.PHONY: deps
deps:
	@echo "📦 Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Update dependencies
.PHONY: update
update:
	@echo "🔄 Updating dependencies..."
	$(GOGET) -u ./...
	$(GOMOD) tidy

# Run locally
.PHONY: run
run: build
	@echo "🚀 Running..."
	./$(DIST_DIR)/$(BINARY_NAME)

# Install locally
.PHONY: install
install: build
	@echo "📥 Installing to $(GOPATH)/bin..."
	cp $(DIST_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "✅ Installed to $(GOPATH)/bin/$(BINARY_NAME)"

# Format code
.PHONY: fmt
fmt:
	@echo "📝 Formatting code..."
	$(GOCMD) fmt ./...

# Lint code
.PHONY: lint
lint:
	@echo "🔍 Linting code..."
	@which golangci-lint > /dev/null 2>&1 || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

# Vet code
.PHONY: vet
vet:
	@echo "🔍 Running go vet..."
	$(GOCMD) vet ./...

# Check for issues
.PHONY: check
check: fmt vet lint test
	@echo "✅ All checks passed"

# Generate documentation
.PHONY: docs
docs:
	@echo "📚 Generating documentation..."
	@which godoc > /dev/null 2>&1 || echo "Install godoc: go install golang.org/x/tools/cmd/godoc@latest"
	@echo "Run 'godoc -http=:6060' to view docs"

# Package for release
.PHONY: package
package: build-all
	@echo "📦 Packaging..."
	mkdir -p $(DIST_DIR)/release
	for file in $(DIST_DIR)/$(BINARY_NAME)-*; do \
		if [ -f "$$file" ]; then \
			name=$$(basename "$$file"); \
			cp "$$file" $(DIST_DIR)/release/; \
			sha256sum "$$file" > "$(DIST_DIR)/release/$$name.sha256"; \
		fi \
	done
	@echo "✅ Packaged to $(DIST_DIR)/release/"

# Help
.PHONY: help
help:
	@echo "Prima Distributed Local - Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build          - Build for current platform"
	@echo "  make build-all      - Build for all platforms"
	@echo "  make test           - Run tests"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make deps           - Download dependencies"
	@echo "  make run            - Build and run"
	@echo "  make install        - Install to GOPATH/bin"
	@echo "  make package        - Package for release"
	@echo ""
	@echo "Platform-specific builds:"
	@echo "  make build-linux        - Build all Linux variants"
	@echo "  make build-linux-amd64  - Build Linux amd64"
	@echo "  make build-linux-arm64  - Build Linux arm64"
	@echo "  make build-darwin       - Build all macOS variants"
	@echo "  make build-darwin-amd64 - Build macOS Intel"
	@echo "  make build-darwin-arm64 - Build macOS Apple Silicon"
	@echo "  make build-windows      - Build Windows amd64"

# Default target
.DEFAULT_GOAL := build
