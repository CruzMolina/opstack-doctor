.PHONY: test vet fmt build

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/opstack-doctor
