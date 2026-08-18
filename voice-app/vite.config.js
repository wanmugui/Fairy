import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api/session': { target: 'http://localhost:3080', changeOrigin: true },
      '/api': { target: 'http://localhost:8081', changeOrigin: true },
      '/voice/api': { target: 'http://127.0.0.1:8787', changeOrigin: true, rewrite: p => p.replace(/^\/voice/, '') },
      '/voice-ws': { target: 'ws://127.0.0.1:8788', ws: true, rewrite: () => '/ws/stt' },
    },
  },
})