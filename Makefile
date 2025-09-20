# Makefile for IBP GeoDNS Monitor

# Variables
BINARY_NAME=ibp-monitor
MAIN_PATH=src/IBPMonitor.go
BUILD_DIR=bin
CONFIG_FILE=config/ibpmonitor.json
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%d_%H:%M:%S")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
NC=\033[0m # No Color

.PHONY: all build clean test deps run fmt vet lint install help

## help: Display this help message
help:
	@echo "IBP GeoDNS Monitor Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make <target>"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  ${GREEN}%-15s${NC} %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## all: Build the binary
all: build

## build: Build the monitor binary
build:
	@echo "${GREEN}Building $(BINARY_NAME)...${NC}"
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "${GREEN}Build complete: $(BUILD_DIR)/$(BINARY_NAME)${NC}"

## build-linux: Build for Linux
build-linux:
	@echo "${GREEN}Building $(BINARY_NAME) for Linux...${NC}"
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	@echo "${GREEN}Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64${NC}"

## build-windows: Build for Windows
build-windows:
	@echo "${GREEN}Building $(BINARY_NAME) for Windows...${NC}"
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	@echo "${GREEN}Build complete: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe${NC}"

## build-all: Build for all platforms
build-all: build build-linux build-windows

## clean: Remove build artifacts
clean:
	@echo "${YELLOW}Cleaning...${NC}"
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	@echo "${GREEN}Clean complete${NC}"

## test: Run tests
test:
	@echo "${GREEN}Running tests...${NC}"
	$(GOTEST) -v ./src/...
	@echo "${GREEN}Tests complete${NC}"

## deps: Download and verify dependencies
deps:
	@echo "${GREEN}Downloading dependencies...${NC}"
	$(GOMOD) download
	$(GOMOD) verify
	@echo "${GREEN}Dependencies ready${NC}"

## tidy: Tidy and vendor dependencies
tidy:
	@echo "${GREEN}Tidying dependencies...${NC}"
	$(GOMOD) tidy
	@echo "${GREEN}Dependencies tidied${NC}"

## fmt: Format code
fmt:
	@echo "${GREEN}Formatting code...${NC}"
	$(GOFMT) ./...
	@echo "${GREEN}Code formatted${NC}"

## vet: Run go vet
vet:
	@echo "${GREEN}Running go vet...${NC}"
	$(GOCMD) vet ./...
	@echo "${GREEN}Vet complete${NC}"

## lint: Run linter (requires golangci-lint)
lint:
	@echo "${GREEN}Running linter...${NC}"
	@which golangci-lint > /dev/null || (echo "${RED}golangci-lint not installed${NC}" && exit 1)
	golangci-lint run
	@echo "${GREEN}Lint complete${NC}"

## run: Build and run the monitor
run: build
	@echo "${GREEN}Starting $(BINARY_NAME)...${NC}"
	./$(BUILD_DIR)/$(BINARY_NAME) -config $(CONFIG_FILE)

## install: Install the binary to /usr/local/bin
install: build
	@echo "${GREEN}Installing $(BINARY_NAME) to /usr/local/bin...${NC}"
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "${GREEN}Installation complete${NC}"

## uninstall: Remove the binary from /usr/local/bin
uninstall:
	@echo "${YELLOW}Removing $(BINARY_NAME) from /usr/local/bin...${NC}"
	sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "${GREEN}Uninstall complete${NC}"

## docker-build: Build Docker image
docker-build:
	@echo "${GREEN}Building Docker image...${NC}"
	docker build -t ibp-geodns-monitor:$(VERSION) .
	@echo "${GREEN}Docker build complete${NC}"

## check: Run fmt, vet, and test
check: fmt vet test

# Default target
.DEFAULT_GOAL := help