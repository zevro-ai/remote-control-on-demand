BINARY := rcod

.PHONY: build test vet fmt clean

build:
	go build -o $(BINARY) ./cmd/bot

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -f $(BINARY)
