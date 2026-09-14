# Build
FROM golang:1.26.7-bookworm AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/api \
    ./cmd/api

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/sqs-init \
    ./cmd/sqs-init

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/worker \
    ./cmd/worker


# Test
FROM build-stage AS run-test-stage

RUN go test -v ./...

# Deploy
FROM gcr.io/distroless/base-debian12 AS build-release-stage

COPY --from=run-test-stage /out/api /api
COPY --from=run-test-stage /out/sqs-init /sqs-init
COPY --from=run-test-stage /out/worker /worker

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/api"]