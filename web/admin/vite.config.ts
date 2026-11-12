import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Go server serves the build at /admin/. In dev, API calls go to a local server.
export default defineConfig({
  base: "/admin/",
  plugins: [react()],
  server: { proxy: { "/admin/api": "http://127.0.0.1:8088" } },
});
