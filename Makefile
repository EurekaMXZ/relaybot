APP=relaybot

.PHONY: run test fmt

run:
	go run ./cmd/relaybot

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

