import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// Backend the dev server proxies to. `bun run dev` (scripts/dev.ts) sets it
// when port 3000 is taken.
const apiTarget = process.env.GOWA_DEV_API_URL || 'http://127.0.0.1:3000'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    outDir: '../public',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/app': {
        target: apiTarget,
        // Keep the browser's Host: the manager matches it against Origin to
        // let GOWA accept requests from its admin page, and uses it for
        // redirects and the GOWA UI base URL.
        changeOrigin: false,
        ws: true,
      },
    },
  },
})
