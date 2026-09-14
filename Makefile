include .env
export

.PHONY: run fmt test test_race test_full test_unit test_integration test_e2e test_all \
        test_e2e_up test_e2e_down \
        is_live is_ready

run:
	go run ./cmd/api

fmt:
	gofmt -w .

test:
	go test ./...

test_race:
	go test -race ./...

test_full:
	go test -race -cover ./...

test_unit:
	go test -race -count=1 ./...

test_integration:
	@echo "Running integration tests (requires TEST_DATABASE_URL)..."
	@echo "Start test DB: docker compose -f docker-compose.test.yml up -d postgres migrate"
	@echo "Then set: export TEST_DATABASE_URL=postgresql://api_test:api_test@localhost:5433/api_test?sslmode=disable"
	go test -v -race -count=1 -tags=integration -timeout=300s ./...

test_e2e:
	@echo "Running e2e tests (full stack)..."
	docker compose -f docker-compose.test.yml down -v --remove-orphans 2>/dev/null || true
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from test-runner
	docker compose -f docker-compose.test.yml down -v --remove-orphans

test_all: test_unit test_e2e

is_live:
	curl -i http://localhost:8080/health/live

is_ready:
	curl -i http://localhost:8080/health/ready

## Migrations
CONN := "postgresql://$(API_DB_USER):$(API_DB_PWD)@localhost:$(API_DB_PORT)/$(API_DB)?sslmode=disable"
FOLDER := ./migrations

.PHONY: migrate_up migrate_down migrate_create migrate_force

migrate_create:
	@if [ -z "$(VAL)" ]; then\
	    echo "VAL is required. Usage: make create VAL=NEW_NAME"; exit 1;\
	fi
	@echo "Running migrate create $(VAL)"
	migrate create -ext sql -dir $(FOLDER) -seq $(VAL)

migrate_up:
	@if [ -z "$(VAL)" ]; then \
	    echo "Running just migrate up"; \
	    migrate -path "$(FOLDER)" -database "$(CONN)" up; \
	else \
	    echo "Running migrate up $(VAL)"; \
	    migrate -path "$(FOLDER)" -database "$(CONN)" up "$(VAL)"; \
	fi

migrate_down_all:
	@echo "Running just migrate down"
	migrate -path "$(FOLDER)" -database "$(CONN)" down

migrate_down: VAL=1
migrate_down:
	@echo "Running migrate down $(VAL)"
	migrate -path "$(FOLDER)" -database "$(CONN)" down "$(VAL)"

migrate_force:
	@if [ -z "$(VAL)" ]; then\
	    echo "VAL is required. Usage: make force VAL=YOUR_STEP"; exit 1;\
	fi
	@echo "Running migrate force $(VAL)"
	migrate -path $(FOLDER) -database "$(CONN)" force $(VAL)
