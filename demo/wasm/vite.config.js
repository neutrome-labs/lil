import { defineConfig } from 'vite';

export default defineConfig({
  assetsInclude: ['**/*.wasm'],
  build: {
    assetsInlineLimit: 0,
    emptyOutDir: true,
    outDir: 'dist',
    sourcemap: true,
  },
});
