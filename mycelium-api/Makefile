# Mycelium API — Cross-Compilation Makefile
# Usage:
#   make build          Build for current platform
#   make build-all      Build for all Coven devices
#   make deploy          Create deployment packages
#   make clean          Remove build artifacts

BINARY_NAME = mycelium-api
CMD = ./cmd/mycelium-api

# Version from git tag or "dev"
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w -X main.version=$(VERSION)

# Coven device targets
DEVICES = shepherd-dell-latitude rhubarb-rpi5 crow-wren-rpi-zero2w owl-rpi-model-b

# Architecture mapping
SHEPHERD_ARCH = linux/amd64
RHUBARB_ARCH = linux/arm64
CROW_WREN_OWL_ARCH = linux/arm

.PHONY: build build-all clean deploy

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(CMD)

build-shepherd:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-amd64 $(CMD)

build-rhubarb:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-arm64 $(CMD)

build-crow-wren-owl:
	GOOS=linux GOARCH=arm GOARM=6 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-armhf $(CMD)

build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-windows-amd64.exe $(CMD)

build-all: build-shepherd build-rhubarb build-crow-wren-owl build-windows
	@echo "All binaries built in dist/"

deploy: build-all
	@for device in shepherd-dell-latitude rhubarb-rpi5 crow-wren-rpi-zero2w owl-rpi-model-b; do \
		mkdir -p dist/$$device; \
		cp mycelium dist/$$device/mycelium; \
		cp configs/mycelium.yaml dist/$$device/mycelium.yaml; \
		cp scripts/README-$$device.md dist/$$device/README.md 2>/dev/null || true; \
	done
	cp mycelium dist/shepherd-dell-latitude/mycelium-api-linux-amd64
	cp mycelium dist/rhubarb-rpi5/mycelium-api-linux-arm64
	cp mycelium dist/crow-wren-rpi-zero2w/mycelium-api-linux-armhf
	cp mycelium dist/owl-rpi-model-b/mycelium-api-linux-armhf
	@echo "Deployment packages ready in dist/"

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe
	rm -rf dist/
