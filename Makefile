BINARY := rcod

.PHONY: build test vet fmt clean package-deb

build:
	cd app && npm ci && npm run build
	go build -o $(BINARY) ./cmd/rcodbot
	go build -o rcod-agent ./cmd/rcod-agent

test:
	cd app && npm ci && npm test
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -f $(BINARY) rcod-agent

package-deb:
	goreleaser release --snapshot --clean --skip=publish
