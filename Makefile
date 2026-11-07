GO ?= go
VERSION ?= dev

-include .env
export RANJIT_API_KEY RANJIT_ADDR RANJIT_BASE_URL RANJIT_DATABASE RANJIT_PROVIDER RANJIT_SYNC_INTERVAL
export RANJIT_EFI_ENV RANJIT_EFI_CLIENT_ID RANJIT_EFI_CLIENT_SECRET_FILE RANJIT_EFI_CERT_FILE RANJIT_EFI_KEY_FILE RANJIT_EFI_LOOKBACK_DAYS

.PHONY: setup run build test vet fmt check example smoke

setup:
	$(GO) run ./cmd/ranjit init

run:
	$(GO) run ./cmd/ranjit

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/ranjit ./cmd/ranjit

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

check: test vet
	test -z "$$(gofmt -l .)"

example:
	$(GO) run ./examples/connect

smoke:
	python3 scripts/smoke.py
