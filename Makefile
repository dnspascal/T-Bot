DB_URL ?= postgres://localhost/tbot_local?sslmode=disable

.PHONY: migrate-up migrate-down migrate-create run build

migrate-up:
	go run ./cmd/migrate

migrate-down:
	migrate -path cmd/migrate/migrations -database "$(DB_URL)" down

build:
	go build -o bin/bot ./cmd/bot

run:
	go run ./cmd/bot

migrate-create:
	@read -p "Name: " name; \
	num=$$(ls cmd/migrate/migrations/*.up.sql 2>/dev/null | wc -l | tr -d ' '); \
	num=$$(printf "%06d" $$((num + 1))); \
	touch cmd/migrate/migrations/$${num}_$${name}.up.sql; \
	touch cmd/migrate/migrations/$${num}_$${name}.down.sql; \
	echo "Created $${num}_$${name}"
