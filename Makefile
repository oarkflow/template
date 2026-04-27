SHELL := /bin/bash

ROOT_DIR := $(abspath .)
FIBER_DIR := $(ROOT_DIR)/examples/fiber
STATIC_DIR := $(FIBER_DIR)/static
GENERATED_DIR := $(STATIC_DIR)/generated
WASM_FILE := $(GENERATED_DIR)/spl.wasm
WASM_GZ := $(GENERATED_DIR)/spl.wasm.gz
WASM_EXEC := $(GENERATED_DIR)/wasm_exec.js
BUILD_CACHE_DIR := $(ROOT_DIR)/.cache/go-build
XDG_CACHE_DIR := $(ROOT_DIR)/.cache
TINYGO_OPT ?= z
VERBOSE ?= 0
TINYGO_VERBOSE_FLAGS :=

ifeq ($(VERBOSE),1)
TINYGO_VERBOSE_FLAGS += -x -work
endif

.PHONY: help fmt test build build-fiber run-fiber wasm wasm-go wasm-tinygo wasm-clean clean

help:
	@echo "Available targets:"
	@echo "  make fmt          - gofmt the template module"
	@echo "  make test         - run go test ./..."
	@echo "  make build        - build the template module"
	@echo "  make build-fiber  - build the Fiber showcase"
	@echo "  make run-fiber    - run the Fiber showcase"
	@echo "  make wasm         - build a KB-class wasm asset with TinyGo"
	@echo "                    - use VERBOSE=1 for TinyGo command details"
	@echo "  make wasm-go      - build a standard Go wasm asset (MB-class fallback)"
	@echo "  make wasm-clean   - remove generated wasm assets"
	@echo "  make clean        - remove generated wasm assets and local binaries"

fmt:
	gofmt -w *.go cmd/splwasm/main.go examples/fiber/main.go

test:
	go test ./...

build:
	go build ./...

build-fiber:
	cd $(FIBER_DIR) && go build ./...

run-fiber:
	cd $(FIBER_DIR) && go run .

wasm: wasm-tinygo

wasm-go:
	@mkdir -p "$(GENERATED_DIR)"
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o "$(WASM_FILE)" ./cmd/splwasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" "$(WASM_EXEC)"
	gzip -kf -9 "$(WASM_FILE)"
	@echo "Generated standard Go wasm assets:"
	@ls -lh "$(WASM_FILE)" "$(WASM_GZ)" "$(WASM_EXEC)"

wasm-tinygo:
	@mkdir -p "$(GENERATED_DIR)" "$(BUILD_CACHE_DIR)" "$(XDG_CACHE_DIR)"
	@echo "Building TinyGo wasm with -opt $(TINYGO_OPT) ..."
	@echo "This step can be quiet for a while during TinyGo/LLVM compilation."
	@if [ "$(VERBOSE)" = "1" ]; then echo "Verbose TinyGo logging enabled."; fi
	@env XDG_CACHE_HOME="$(XDG_CACHE_DIR)" GOCACHE="$(BUILD_CACHE_DIR)" \
		tinygo build $(TINYGO_VERBOSE_FLAGS) -o "$(WASM_FILE)" -target wasm -opt $(TINYGO_OPT) ./cmd/splwasm || { \
		echo ""; \
		echo "TinyGo build failed."; \
		echo "If you need a KB-class wasm, use a TinyGo-compatible Go toolchain."; \
		echo "Fallback: make wasm-go"; \
		exit 1; \
	}
	cp "$$(tinygo env TINYGOROOT)/targets/wasm_exec.js" "$(WASM_EXEC)"
	gzip -kf -9 "$(WASM_FILE)"
	@echo "Generated TinyGo wasm assets:"
	@ls -lh "$(WASM_FILE)" "$(WASM_GZ)" "$(WASM_EXEC)"

wasm-clean:
	rm -rf "$(GENERATED_DIR)"

clean: wasm-clean
	rm -rf "$(ROOT_DIR)/.cache"
	rm -f "$(ROOT_DIR)/splwasm" "$(FIBER_DIR)/fiber"
