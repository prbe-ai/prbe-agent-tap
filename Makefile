.PHONY: build test e2e vet fmt clean

VERSION ?= dev
LDFLAGS := -ldflags "-X github.com/prbe-ai/prbe-agent-tap/internal/version.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o prbe-agent-tap ./cmd/prbe-agent-tap

test:
	go test -race ./...

e2e:
	go test -race -tags=e2e ./tests/e2e/...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -f prbe-agent-tap
	rm -rf dist/
