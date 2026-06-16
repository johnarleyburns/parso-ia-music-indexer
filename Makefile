.PHONY: build run headless clean

build:
	go build -o bin/timbre ./cmd/tui

run: build
	./bin/timbre

headless: build
	./bin/timbre --headless

clean:
	rm -rf bin/
