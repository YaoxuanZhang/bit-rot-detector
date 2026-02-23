import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/api/static',
    // Clean old hashed bundles before each build so stale files don't
    // accumulate in the committed static/ tree.  Vite requires explicit
    // opt-in when outDir lives outside the project root.
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
