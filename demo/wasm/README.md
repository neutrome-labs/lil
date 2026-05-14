# AIL WASM demo

Client-side static app for converting between AIL-supported LLM API formats.
The Go converter runs in WebAssembly in the browser; there is no server API.

```sh
npm install
npm run dev
```

Production build:

```sh
npm run build
```

The root `make demo-wasm` target builds `src/demo.wasm` and copies Go's
`wasm_exec.js` bridge into `src/` before Vite bundles the app.
