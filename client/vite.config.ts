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
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
