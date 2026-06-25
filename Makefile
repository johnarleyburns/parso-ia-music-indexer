.PHONY: build run headless clean proto test test-sidecar ci install-hooks export-ios eval-retrieval

build:
	go build -o bin/timbre ./cmd/tui

run: build
	./bin/timbre

headless: build
	./bin/timbre --headless

test:
	go test ./...

ci: build
	go vet ./...
	go test -race -count=1 ./...

install-hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks installed from .githooks/"

test-sidecar:
	cd python_sidecar && .venv/bin/python test_inference.py

proto:
	PATH="$$HOME/go/bin:$$PATH" protoc \
		--go_out=. --go_opt=module=github.com/johnarleyburns/parso-ia-music-indexer \
		--go-grpc_out=. --go-grpc_opt=module=github.com/johnarleyburns/parso-ia-music-indexer \
		proto/clap.proto
	python_sidecar/.venv/bin/python -m grpc_tools.protoc \
		--python_out=python_sidecar --grpc_python_out=python_sidecar \
		-Iproto proto/clap.proto

export-ios:
	cd python_sidecar && .venv/bin/python export_for_ios.py \
		--output-dir ../../parso-acalum-ios-app/Acalum/Resources/
	@echo "Exported CLAP text encoder + tokenizer + test_vectors.json to Acalum/Resources/"

eval-retrieval:
	cd python_sidecar && .venv/bin/python eval_retrieval.py --db ../data/parso_indexer.db --top-k 10

clean:
	rm -rf bin/
