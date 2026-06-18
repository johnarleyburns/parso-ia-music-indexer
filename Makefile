.PHONY: build run headless clean proto test

build:
	go build -o bin/timbre ./cmd/tui

run: build
	./bin/timbre

headless: build
	./bin/timbre --headless

test:
	go test ./...

proto:
	PATH="$$HOME/go/bin:$$PATH" protoc \
		--go_out=. --go_opt=module=github.com/johnarleyburns/parso-ia-music-indexer \
		--go-grpc_out=. --go-grpc_opt=module=github.com/johnarleyburns/parso-ia-music-indexer \
		proto/clap.proto
	python_sidecar/.venv/bin/python -m grpc_tools.protoc \
		--python_out=python_sidecar --grpc_python_out=python_sidecar \
		-Iproto proto/clap.proto

clean:
	rm -rf bin/
