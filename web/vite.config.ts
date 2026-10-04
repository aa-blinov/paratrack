import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

const projectRoot = fileURLToPath(new URL(".", import.meta.url))

export default defineConfig({
  root: projectRoot,
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    outDir: fileURLToPath(new URL("../internal/web/static/ui", import.meta.url)),
    emptyOutDir: true,
    cssCodeSplit: false,
    rollupOptions: {
      input: fileURLToPath(new URL("./src/main.tsx", import.meta.url)),
      output: {
        entryFileNames: "app.js",
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: "app[extname]",
        manualChunks(id) {
          if (id.includes("/node_modules/")) return "vendor"
          if (id.includes("/src/components/ui/") || id.endsWith("/src/lib/utils.ts") || id.endsWith("/src/i18n.ts")) return "ui"
        },
      },
    },
  },
})
