.PHONY: run build docker-build test test-race test-integration lint fmt proto proto-lint proto-breaking proto-check swagger swagger-check migrate-up migrate-down dev-up dev-down dev-logs

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOPROXY ?= https://proxy.golang.org,direct
LDFLAGS = -s -w -X github.com/lihongjie0209/audit-service/internal/buildinfo.Version=$(VERSION) -X github.com/lihongjie0209/audit-service/internal/buildinfo.Commit=$(COMMIT) -X github.com/lihongjie0209/audit-service/internal/buildinfo.BuildTime=$(BUILD_TIME)

run:
	go run ./cmd/api -config config/config.yaml

build:
	go build -ldflags="$(LDFLAGS)" -o bin/api ./cmd/api
	go build -trimpath -o bin/migrate ./cmd/migrate

docker-build:
	docker build \
		--build-arg GOPROXY="$(GOPROXY)" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD_TIME="$(BUILD_TIME)" \
		--tag audit-service:$(VERSION) .

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration -count=1 -timeout=15m ./integration/...

dev-up:
	VERSION="$(VERSION)" COMMIT="$(COMMIT)" BUILD_TIME="$(BUILD_TIME)" docker compose up --build -d --wait

dev-down:
	docker compose down --remove-orphans

dev-logs:
	docker compose logs -f api

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .

swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/api/main.go -o docs --parseInternal

swagger-check:
	@tmp_dir=$$(mktemp -d); trap 'rm -rf "$$tmp_dir"' EXIT; cp -R docs "$$tmp_dir/docs"; $(MAKE) swagger; diff -ru "$$tmp_dir/docs" docs

proto:
	buf generate

proto-lint:
	buf lint

proto-breaking:
	buf breaking --against '.git#ref=HEAD^' --against-config buf.yaml

proto-check: proto-lint proto
	git diff --exit-code -- gen

migrate-up:
	go run ./cmd/migrate -direction up

migrate-down:
	go run ./cmd/migrate -direction down
