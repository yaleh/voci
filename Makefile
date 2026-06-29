BIN     := voci
CMD     := ./cmd/voci
GOFLAGS :=

.PHONY: build install test clean

build:
	go build $(GOFLAGS) -o $(BIN) $(CMD)

install:
	go install $(GOFLAGS) $(CMD)

test:
	go test ./...

clean:
	rm -f $(BIN)
