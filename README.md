# SPL Template Engine

This repo now supports two frontend modes:

- Server/SSR mode: the existing Go-side renderer and hydration runtime.
- Browser/WASM mode: the browser fetches a template bundle plus JSON data and renders `views/index.html` entirely on the client.

The Fiber showcase keeps both:

- `/` for the current SSR demo
- `/browser` for the browser-rendered WASM showcase

## Requirements

- Go `1.26.1`
- The sibling interpreter repo at `/home/sujit/Projects/interpreter`
- For KB-class wasm builds: `tinygo`

`go.mod` is intentionally wired to the local interpreter checkout:

```text
replace github.com/oarkflow/interpreter => ../interpreter
```

The Fiber example has its own matching replace.

## Quick Start

Build and run the main module:

```bash
make build
make test
```

Run the Fiber demo:

```bash
make run-fiber
```

Then open:

- `http://localhost:3000/`
- `http://localhost:3000/browser`

## WASM Builds

The Fiber browser route serves these generated assets when present:

- `examples/fiber/static/generated/spl.wasm`
- `examples/fiber/static/generated/spl.wasm.gz`
- `examples/fiber/static/generated/wasm_exec.js`

### Preferred: TinyGo

For a KB-class wasm asset, build with TinyGo:

```bash
make wasm
```

This target:

- builds `cmd/splwasm` with TinyGo
- copies the matching TinyGo `wasm_exec.js`
- writes a gzipped wasm alongside it

### Fallback: Standard Go

If TinyGo cannot be used yet, build the larger standard-Go variant:

```bash
make wasm-go
```

That output is much larger:

- raw wasm: about `17 MB` in this environment
- gzipped wasm: about `4.2 MB`

### Important TinyGo Note

On this machine, `tinygo 0.31.2` is installed but currently rejects `go1.26.1` with:

```text
requires go version 1.18 through 1.22, got go1.26
```

So if you want the wasm file to be in KB, you need a TinyGo-compatible Go toolchain for the TinyGo build path. The codebase and Makefile are set up for that path already; the remaining blocker is local toolchain compatibility.

## Make Targets

```bash
make help
make fmt
make test
make build
make build-fiber
make run-fiber
make wasm
make wasm-go
make wasm-clean
make clean
```

## Browser/WASM Flow

The `/browser` route works like this:

1. Fiber serves a shell page with `#app`.
2. The browser loads `/assets/wasm_exec.js` and `/assets/browser-app.js`.
3. The browser fetches:
   - `/assets/spl-bundle.json`
   - `/api/browser/page-data`
4. `cmd/splwasm` loads the bundle into the template engine.
5. The browser renderer keeps signal state client-side and rerenders the full root after:
   - `@signal` updates
   - `on:*` handlers
   - `data-spl-model` changes
   - `data-spl-api-*` responses

## Files To Know

- [browser_bundle.go](./browser_bundle.go)
  Browser bundle loading, in-memory templates, and browser signal state.
- [cmd/splwasm/main.go](./cmd/splwasm/main.go)
  Go WASM entrypoint exposed to the browser.
- [examples/fiber/static/browser-app.js](./examples/fiber/static/browser-app.js)
  DOM bridge for events, models, API requests, and rerenders.
- [examples/fiber/main.go](./examples/fiber/main.go)
  SSR route, browser route, bundle route, wasm asset routes, and page-data API.

## Notes

- If generated wasm assets are missing, the Fiber server falls back to building a standard-Go wasm on demand.
- If generated assets exist, the server prefers them and serves the gzipped wasm when the browser supports gzip.
- Existing SSR and hydration behavior remains intact for the `/` route.
