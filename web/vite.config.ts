import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: true,
    allowedHosts: ['mini-1.stout.zone'],
    proxy: {
      '/api': {
        target: 'http://localhost:6969',
        changeOrigin: true,
      },
    },
  },
})
