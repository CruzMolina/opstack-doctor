VERSION ?= $(shell cat VERSION)
IMAGE ?= opstack-doctor:$(VERSION)
BIN ?= opstack-doctor

.PHONY: test vet fmt fmt-check version-check build clean demo-smoke release-check docker-build docker-smoke

fmt:
	gofmt -w ./cmd ./internal

fmt-check:
	test -z "$$(gofmt -l ./cmd ./internal)"

version-check:
	test "$$(go run ./cmd/opstack-doctor version)" = "opstack-doctor v$(VERSION)"

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -ldflags="-X main.version=$(VERSION)" -o $(BIN) ./cmd/opstack-doctor

clean:
	rm -f $(BIN)

demo-smoke:
	go run ./cmd/opstack-doctor version
	go run ./cmd/opstack-doctor demo --scenario healthy --output prometheus >/tmp/opstack-doctor-demo-healthy.prom
	go run ./cmd/opstack-doctor demo --scenario warn --output json >/tmp/opstack-doctor-demo-warn.json
	go run ./cmd/opstack-doctor demo --scenario fail --output prometheus >/tmp/opstack-doctor-demo-fail.prom

release-check: fmt-check version-check test vet build demo-smoke

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

docker-smoke:
	docker run --rm $(IMAGE) version
	docker run --rm $(IMAGE) demo --scenario healthy --output prometheus >/tmp/opstack-doctor-docker-demo-healthy.prom
