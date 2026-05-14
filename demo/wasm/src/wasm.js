import wasmURL from './demo.wasm?url';
import './wasm_exec.js';

let readyPromise;

export function loadAILWasm() {
  if (readyPromise) return readyPromise;

  readyPromise = (async () => {
    const go = new globalThis.Go();
    const response = await fetch(wasmURL);
    let result;

    try {
      result = await WebAssembly.instantiateStreaming(response.clone(), go.importObject);
    } catch {
      const bytes = await response.arrayBuffer();
      result = await WebAssembly.instantiate(bytes, go.importObject);
    }

    go.run(result.instance);
  })();

  return readyPromise;
}

export async function convertAIL(request) {
  await loadAILWasm();
  if (typeof globalThis.convertAIL !== 'function') {
    throw new Error('AIL WASM bridge did not register convertAIL');
  }
  return globalThis.convertAIL(request);
}
