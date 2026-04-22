# Project variables
BINARY_NAME=skill-runner
CMD_DIR=./cmd/skill-runner
OCI_BINARY_NAME=skill-runner-oci
OCI_CMD_DIR=./cmd/skill-runner-oci
PKG=github.com/mdfranz/skill-runner

# Build configuration
GO=go
GOFLAGS=-v
LDFLAGS=-ldflags="-s -w"

# Image variables
IMAGE_DIR=./image
IMAGE_TAG=skill-runner-image

.PHONY: all build build-oci build-all test clean lint run help image image-clean

all: build build-oci

## build: Build the namespace-sandbox binary (skill-runner)
build:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME) $(CMD_DIR)

## build-oci: Build the OCI-runtime binary (skill-runner-oci)
build-oci:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(OCI_BINARY_NAME) $(OCI_CMD_DIR)

## build-all: Build both binaries
build-all: build build-oci

## image: Build the OCI runtime image from Containerfile using buildah (daemonless)
image:
	@command -v buildah >/dev/null || { echo "buildah not installed (apt install buildah / dnf install buildah)"; exit 1; }
	buildah build --isolation=chroot --format oci -t $(IMAGE_TAG) -f Containerfile .
	rm -rf $(IMAGE_DIR)
	buildah push $(IMAGE_TAG) oci:$(IMAGE_DIR):latest
	@echo "OCI layout written to $(IMAGE_DIR)"

## image-clean: Remove the unpacked OCI image layout
image-clean:
	rm -rf $(IMAGE_DIR)

## test: Run tests
test:
	$(GO) test -v ./...

## clean: Remove build artifacts (binaries only; use image-clean for the image)
clean:
	rm -f $(BINARY_NAME) $(OCI_BINARY_NAME)
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
