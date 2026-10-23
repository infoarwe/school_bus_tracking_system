import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Throwaway dev config for manually testing the multiple-trip-scheduling
// feature against the disposable sbts_test database, side by side with the
// real dev setup (vite.config.ts, port 5180 -> API :8085). Never imported by
// anything else; delete this file when done. Run with:
//   npx vite --config vite.config.test8086.ts --host 0.0.0.0
const target = 'http://localhost:8086'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5181,
    strictPort: true,
    proxy: {
      '/api': {
        target,
        changeOrigin: true,
        ws: true,
        configure: (proxy) => proxy.on('proxyReqWs', (proxyReq) => proxyReq.setHeader('origin', target)),
      },
      '/health': { target, changeOrigin: true },
    },
  },
})
