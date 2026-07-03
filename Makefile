APP_NAME=llm-gateway
BIN_DIR=bin
GO_BUILD_CACHE?=$(CURDIR)/.cache/go-build
.DEFAULT_GOAL := help

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
ifneq ($(strip $(VERIFY_MODEL)),)
SETUP_ARGS += --model $(VERIFY_MODEL)
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
ifneq ($(filter 1 true yes,$(INSTALL_GCLOUD)),)
SETUP_ARGS += --install-gcloud
endif
ifneq ($(filter 1 true yes,$(OPEN)),)
SETUP_ARGS += --open
endif
ifneq ($(filter 1 true yes,$(PROTECTED)),)
SETUP_ARGS += --protected
endif

PRODUCTION_SETUP_ARGS=
ifneq ($(strip $(PROJECT)),)
PRODUCTION_SETUP_ARGS += --project $(PROJECT)
endif
ifneq ($(strip $(LOCATION)),)
PRODUCTION_SETUP_ARGS += --location $(LOCATION)
endif
ifneq ($(strip $(MODEL)),)
PRODUCTION_SETUP_ARGS += --model $(MODEL)
endif
ifneq ($(strip $(VERIFY_MODEL)),)
PRODUCTION_SETUP_ARGS += --model $(VERIFY_MODEL)
endif
ifneq ($(strip $(SERVICE_ACCOUNT_NAME)),)
PRODUCTION_SETUP_ARGS += --service-account $(SERVICE_ACCOUNT_NAME)
endif
ifneq ($(strip $(KEY_PATH)),)
PRODUCTION_SETUP_ARGS += --key-path $(KEY_PATH)
endif
ifneq ($(strip $(API_KEY)),)
PRODUCTION_SETUP_ARGS += --api-key $(API_KEY)
endif
ifneq ($(filter 1 true yes,$(NON_INTERACTIVE)),)
PRODUCTION_SETUP_ARGS += --non-interactive
endif
ifneq ($(filter 1 true yes,$(SKIP_VERIFY)),)
PRODUCTION_SETUP_ARGS += --skip-verify
endif
ifneq ($(filter 1 true yes,$(INSTALL_GCLOUD)),)
PRODUCTION_SETUP_ARGS += --install-gcloud
endif

SWITCH_ARGS=
ifneq ($(strip $(AUTH)),)
SWITCH_ARGS += --auth $(AUTH)
endif
ifneq ($(strip $(MODE)),)
SWITCH_ARGS += --auth $(MODE)
endif
ifneq ($(strip $(KEY_PATH)),)
SWITCH_ARGS += --key-path $(KEY_PATH)
endif
ifneq ($(strip $(MODEL)),)
SWITCH_ARGS += --model $(MODEL)
endif
ifneq ($(strip $(VERIFY_MODEL)),)
SWITCH_ARGS += --model $(VERIFY_MODEL)
endif
ifneq ($(filter 1 true yes,$(NON_INTERACTIVE)),)
SWITCH_ARGS += --non-interactive
endif

CLOUD_SETUP_ARGS=
ifneq ($(strip $(PROJECT)),)
CLOUD_SETUP_ARGS += --project $(PROJECT)
endif
ifneq ($(strip $(LOCATION)),)
CLOUD_SETUP_ARGS += --location $(LOCATION)
endif
ifneq ($(strip $(MODEL)),)
CLOUD_SETUP_ARGS += --model $(MODEL)
endif
ifneq ($(strip $(REGION)),)
CLOUD_SETUP_ARGS += --region $(REGION)
endif
ifneq ($(strip $(SERVICE)),)
CLOUD_SETUP_ARGS += --service $(SERVICE)
endif
ifneq ($(filter 1 true yes,$(DEPLOY)),)
CLOUD_SETUP_ARGS += --deploy
endif
ifneq ($(filter 1 true yes,$(NON_INTERACTIVE)),)
CLOUD_SETUP_ARGS += --non-interactive
endif

.PHONY: help setup setup-production production setup-cloud cloud switch run build test test-race test-live clean fmt verify-gcp

help:
	@echo "Byto Gateway"
	@echo
	@echo "Use one of these:"
	@echo "  make setup PROJECT=your-gcp-project"
	@echo "  make setup production PROJECT=your-gcp-project MODEL=gemini-2.5-flash"
	@echo "  make switch AUTH=service MODEL=gemini-2.5-flash"
	@echo "  make switch AUTH=token MODEL=gemini-2.5-flash"
	@echo "  make setup PROJECT=your-gcp-project OPEN=1"
	@echo "  make setup PROJECT=your-gcp-project PROTECTED=1"
	@echo "  make setup-cloud PROJECT=your-gcp-project MODEL=gemini-2.5-flash"
	@echo
	@echo "Then:"
	@echo "  make run"
	@echo "  make test"
	@echo
	@echo "Cloud deploy:"
	@echo "  make setup-cloud PROJECT=your-gcp-project MODEL=gemini-2.5-flash DEPLOY=1"

setup:
	@if [ -n "$(filter production,$(MAKECMDGOALS))" ] || [ "$(PRODUCTION)" = "1" ]; then \
		./scripts/setup-production.sh $(PRODUCTION_SETUP_ARGS); \
	elif [ -n "$(filter cloud,$(MAKECMDGOALS))" ] || [ "$(CLOUD)" = "1" ]; then \
		./scripts/setup-cloud.sh $(CLOUD_SETUP_ARGS); \
	else \
		./setup.sh $(SETUP_ARGS); \
	fi

setup-production:
	@./scripts/setup-production.sh $(PRODUCTION_SETUP_ARGS)

production:
	@:

setup-cloud:
	@./scripts/setup-cloud.sh $(CLOUD_SETUP_ARGS)

cloud:
	@:

switch:
	@./scripts/switch-auth.sh $(SWITCH_ARGS)

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
	RUN_LIVE_VERTEX_TESTS=1 LIVE_VERTEX_MODEL="$(MODEL)" GOCACHE="$(GO_BUILD_CACHE)" go test ./test/e2e -count=1 -v

fmt:
	gofmt -w cmd internal test

verify-gcp:
	@if [ -z "$(MODEL)" ]; then echo "MODEL is required, e.g. make verify-gcp MODEL=gemini-3.1-pro-preview"; exit 1; fi
	./scripts/verify-vertex.sh "$(MODEL)"

clean:
	rm -rf $(BIN_DIR) coverage.out .cache
