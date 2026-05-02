.PHONY: build go-build web-build web-dev run dev test vet fmt tidy clean

build: web-build go-build

go-build:
	go build -o bin/register-cli ./backend/cmd/register-cli

web-build:
	npm --prefix web run web:build

web-dev:
	npm --prefix web run web:dev

run:
	go run ./backend/cmd/register-cli

dev:
	@set -e; \
	go run ./backend/cmd/register-cli -web -web-addr 127.0.0.1:8080 -db data/accounts.db & \
	go_pid=$$!; \
	trap 'kill $$go_pid 2>/dev/null || true; wait $$go_pid 2>/dev/null || true' EXIT INT TERM; \
	npm --prefix web run web:dev

test:
	go test ./backend/cmd/... ./backend/internal/...

vet:
	go vet ./backend/cmd/... ./backend/internal/...

fmt:
	gofmt -w ./backend/cmd ./backend/internal

tidy:
	go work sync
	go -C backend mod tidy

clean:
	rm -rf bin backend/cmd/register-cli/web/dist
