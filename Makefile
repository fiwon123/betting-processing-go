run:
	go run ./cmd/api

test_echo:
	curl -X POST -d 'hello' http://localhost:8080/echo -w '\n'

test_hello:
	curl -X POST -d 'gopher' http://localhost:8080/hello -w '\n'

format:
	gofmt -w .