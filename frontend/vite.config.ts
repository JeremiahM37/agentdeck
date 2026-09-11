import { defineConfig } from "vite";
import { serviceWorkerPlugin } from "./serviceWorkerPlugin";
export default defineConfig({
  base: "/react/",
  plugins: [
    serviceWorkerPlugin(),
    {
      name: "terminal-dev-route",
      configureServer(server) {
        server.middlewares.use((request, _response, next) => {
          if (request.url?.startsWith("/terminal/"))
            request.url = "/terminal.html";
          next();
        });
      },
    },
  ],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: { input: { app: "index.html", terminal: "terminal.html" } },
  },
  server: { host: "127.0.0.1", port: 19110 },
});
