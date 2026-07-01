BIN     := voci
CMD     := ./cmd/voci
GOFLAGS :=

.PHONY: build install test clean e2e build-web

build: build-web
	go build $(GOFLAGS) -o $(BIN) $(CMD)

build-web:
	npm ci && npm run build

install:
	go install $(GOFLAGS) $(CMD)

test:
	go test ./...

clean:
	rm -f $(BIN)

e2e: build
	cd e2e && npx playwright test
