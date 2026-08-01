import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../assets-new',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // single-file output for Go embed simplicity
        inlineDynamicImports: true,
        entryFileNames: 'app.js',
        assetFileNames: (info) => info.name?.endsWith('.css') ? 'app.css' : info.name,
      },
    },
  },
  server: {
    proxy: {
      '/v1': 'http://127.0.0.1:8080',
    },
  },
})
