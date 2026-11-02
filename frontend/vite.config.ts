import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5172,
    // Web (server) target proxies /api to the Go server so the browser stays
    // same-origin and the HttpOnly session cookie works during dev.
    proxy: {
      '/api': {
        target: 'http://localhost:9900',
        changeOrigin: true,
      },
    },
  },
})
