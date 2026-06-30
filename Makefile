APP_NAME=llm-gateway
BIN_DIR=bin

ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: setup run build test test-race test-live docker docker-prod clean fmt verify-gcp

setup:
	./setup.sh

run:
	go run ./cmd/gateway

build:
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/gateway

test:
	go test ./... -count=1 -v

test-race:
	go test ./... -race -count=1 -v

test-live:
	RUN_LIVE_VERTEX_TESTS=1 go test ./test/e2e -count=1 -v

fmt:
	gofmt -w cmd internal test

verify-gcp:
	./scripts/verify-vertex.sh $${DEFAULT_MODEL:-gemini-3.1-pro-preview}

docker:
	docker build -t $(APP_NAME):local .

docker-prod:
	docker compose -f docker-compose.yml up --build

clean:
	rm -rf $(BIN_DIR) coverage.out
