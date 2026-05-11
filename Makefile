VERSION ?= $(shell cat VERSION)
IMAGE ?= opstack-doctor:$(VERSION)
BIN ?= opstack-doctor
PROMTOOL_IMAGE ?= prom/prometheus:v3.7.3

.PHONY: test vet fmt fmt-check version-check build clean demo-smoke promtool-check promtool-test release-check docker-build docker-smoke

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
	go run ./cmd/opstack-doctor validate --config examples/doctor.example.yaml --output json >/tmp/opstack-doctor-validate-example.json
	go run ./cmd/opstack-doctor demo --scenario healthy --output prometheus >/tmp/opstack-doctor-demo-healthy.prom
	go run ./cmd/opstack-doctor demo --scenario warn --output json >/tmp/opstack-doctor-demo-warn.json
	go run ./cmd/opstack-doctor demo --scenario fail --output prometheus >/tmp/opstack-doctor-demo-fail.prom

promtool-check:
	docker run --rm --entrypoint promtool -v "$$(pwd):/work:ro" --workdir /work $(PROMTOOL_IMAGE) check rules examples/prometheus-rules.example.yaml internal/generate/testdata/alerts.golden.yaml

promtool-test:
	docker run --rm --entrypoint promtool -v "$$(pwd):/work:ro" --workdir /work/examples $(PROMTOOL_IMAGE) test rules prometheus-rules.test.yaml

release-check: fmt-check version-check test vet build demo-smoke promtool-check promtool-test

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

docker-smoke:
	docker run --rm $(IMAGE) version
	docker run --rm -v "$$(pwd)/examples/doctor.example.yaml:/config/doctor.yaml:ro" $(IMAGE) validate --config /config/doctor.yaml --output json >/tmp/opstack-doctor-docker-validate-example.json
	docker run --rm $(IMAGE) demo --scenario healthy --output prometheus >/tmp/opstack-doctor-docker-demo-healthy.prom
