BINARY := rcod

.PHONY: build test vet fmt clean

build:
	cd app && npm ci && npm run build
	go build -o $(BINARY) ./cmd/codexbot

test:
	cd app && npm ci && npm test
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -f $(BINARY)
