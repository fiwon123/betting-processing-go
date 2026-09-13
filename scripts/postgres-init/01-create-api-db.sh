#!/bin/bash
set -e

psql \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --command="CREATE USER \"$API_DB_USER\" WITH PASSWORD '$API_DB_PASSWORD';"

psql \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --command="CREATE DATABASE \"$API_DB\" OWNER \"$API_DB_USER\";"
