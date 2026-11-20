import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  base: '/console/',
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          charts: ['recharts'],
          framework: ['react', 'react-dom', '@tanstack/react-query', '@tanstack/react-router'],
        },
      },
    },
  },
  server: {
    proxy: { '/console/api': 'http://127.0.0.1:8080' },
  },
});
