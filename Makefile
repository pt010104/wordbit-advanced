APP_NAME=api

.PHONY: run test test-integration build migrate-up migrate-down docker-up docker-down

run:
	go run ./cmd/api

test:
	go test ./...

test-integration:
	go test -tags=integration ./internal/repository/postgres

build:
	go build ./cmd/api

migrate-up:
	go run ./cmd/api migrate up

migrate-down:
	go run ./cmd/api migrate down

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v
