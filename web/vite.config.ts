import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  build: { minify: "esbuild", target: "es2022" },
  server: { proxy: { "/api": { target: "http://localhost:1323", changeOrigin: true } } },
});
