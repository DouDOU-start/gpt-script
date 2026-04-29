.PHONY: build run test vet fmt tidy clean

build:
	go build -o bin/register-cli ./cmd/register-cli

run:
	go run ./cmd/register-cli

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

tidy:
	go mod tidy

clean:
	rm -rf bin
