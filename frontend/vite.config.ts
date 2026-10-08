import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev, API calls go through the Vite proxy so the browser sees one origin.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5180,
    strictPort: true,
    proxy: {
      '/api': { target: 'http://localhost:8085', changeOrigin: true, ws: true },
      '/health': { target: 'http://localhost:8085', changeOrigin: true },
    },
  },
})
