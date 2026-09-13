include .env
export

.PHONY: run fmt

run:
	go run ./cmd/api

fmt:
	gofmt -w .


test_health_live:
	curl -i http://localhost:8080/health/live

test_health_ready:
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