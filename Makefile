BIN := bin/andromeda

.PHONY: build test run tidy

build:
	go build -o $(BIN) .

test:
	go test ./...

run:
	go run .

tidy:
	go mod tidy
