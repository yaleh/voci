BIN     := voci
CMD     := ./cmd/voci
GOFLAGS :=

.PHONY: build install test clean e2e

build:
	go build $(GOFLAGS) -o $(BIN) $(CMD)

install:
	go install $(GOFLAGS) $(CMD)

test:
	go test ./...

clean:
	rm -f $(BIN)

e2e: build
	cd e2e && npx playwright test
