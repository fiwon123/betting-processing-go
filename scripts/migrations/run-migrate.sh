#!/bin/sh
exec /migrate -path ./migrations -database "postgres://${API_DB_USER}:${API_DB_PASSWORD}@db:${API_DB_PORT}/${API_DB}?sslmode=disable" up