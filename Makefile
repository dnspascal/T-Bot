DB_URL ?= postgres://localhost/tbot_local?sslmode=disable

.PHONY: migrate-up migrate-down run build

migrate-up:
	go run ./cmd/migrate

migrate-down:
	migrate -path cmd/migrate/migrations -database "$(DB_URL)" down

build:
	go build -o bin/bot ./cmd/bot

run:
	go run ./cmd/bot
