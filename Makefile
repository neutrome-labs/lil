.PHONY: test demo-wasm

test:
	go test -v ./...

demo-wasm:
	mkdir -p ./demo/wasm/src
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o ./demo/wasm/src/demo.wasm ./demo/wasm/wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" ./demo/wasm/src/wasm_exec.js
