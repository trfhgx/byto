APP_NAME=llm-gateway
BIN_DIR=bin
GO_BUILD_CACHE?=$(CURDIR)/.cache/go-build

ifneq (,$(wildcard .env))
include .env
export
endif

SETUP_ARGS=
ifneq ($(strip $(PROJECT)),)
SETUP_ARGS += --project $(PROJECT)
endif
ifneq ($(strip $(LOCATION)),)
SETUP_ARGS += --location $(LOCATION)
endif
ifneq ($(strip $(MODEL)),)
SETUP_ARGS += --model $(MODEL)
endif
ifneq ($(strip $(API_KEY)),)
SETUP_ARGS += --api-key $(API_KEY)
endif
ifneq ($(filter 1 true yes,$(NON_INTERACTIVE)),)
SETUP_ARGS += --non-interactive
endif
ifneq ($(filter 1 true yes,$(SKIP_TESTS)),)
SETUP_ARGS += --skip-tests
endif

.PHONY: help setup run build test test-race test-live docker docker-prod clean fmt verify-gcp

help:
	@echo "Go LLM Gateway"
	@echo
	@echo "Common commands:"
	@echo "  make setup PROJECT=your-gcp-project"
	@echo "  make verify-gcp"
	@echo "  make run"
	@echo "  make test"
	@echo
	@echo "Setup options:"
	@echo "  PROJECT=...          Google Cloud project ID"
	@echo "  LOCATION=global      Vertex AI location"
	@echo "  MODEL=...            Default Gemini model"
	@echo "  API_KEY=...          Gateway API key"
	@echo "  NON_INTERACTIVE=1    Use env/default values"
	@echo "  SKIP_TESTS=1         Skip setup test run"

setup:
	@./setup.sh $(SETUP_ARGS)

run:
	go run ./cmd/gateway

build:
	mkdir -p $(BIN_DIR)
	mkdir -p $(GO_BUILD_CACHE)
	GOCACHE="$(GO_BUILD_CACHE)" go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/gateway

test:
	mkdir -p $(GO_BUILD_CACHE)
	GOCACHE="$(GO_BUILD_CACHE)" go test ./... -count=1 -v

test-race:
	mkdir -p $(GO_BUILD_CACHE)
	GOCACHE="$(GO_BUILD_CACHE)" go test ./... -race -count=1 -v

test-live:
	mkdir -p $(GO_BUILD_CACHE)
	RUN_LIVE_VERTEX_TESTS=1 GOCACHE="$(GO_BUILD_CACHE)" go test ./test/e2e -count=1 -v

fmt:
	gofmt -w cmd internal test

verify-gcp:
	./scripts/verify-vertex.sh $${DEFAULT_MODEL:-gemini-3.1-pro-preview}

docker:
	docker build -t $(APP_NAME):local .

docker-prod:
	docker compose -f docker-compose.yml up --build

clean:
	rm -rf $(BIN_DIR) coverage.out .cache
