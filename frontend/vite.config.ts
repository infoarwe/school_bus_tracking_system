import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev, API calls go through the Vite proxy so the browser sees one origin.
// `npm run dev` uses the local API; `npm run dev:server` uses the shared dev server's
// API (live buses from the real Driver app), with no local API, database or Redis.
const SHARED_DEV_SERVER = 'https://ics.ridelogic.in'

export default defineConfig(({ mode }) => {
  const target = mode === 'server' ? SHARED_DEV_SERVER : 'http://localhost:8085'
  return {
    plugins: [react()],
    server: {
      port: 5180,
      strictPort: true,
      proxy: {
        '/api': {
          target,
          changeOrigin: true,
          ws: true,
          // The server only accepts WebSockets from its own origin.
          configure: (proxy) =>
            proxy.on('proxyReqWs', (proxyReq) => proxyReq.setHeader('origin', target)),
        },
        '/health': { target, changeOrigin: true },
      },
    },
  }
})
