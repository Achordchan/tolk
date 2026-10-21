import { resolve } from "node:path";
import { defineConfig } from "vite";

export default defineConfig({
  server: {
    // The app loads this address during `mygo dev` (devUrl in mygo.json).
    port: 5173,
    strictPort: true,
    // The development app and the packaged builds are not the page's.
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
  build: {
    rollupOptions: {
      // Three pages: the main window, the Mini bubble and the 输入助手 panel.
      input: {
        main: resolve(__dirname, "index.html"),
        mini: resolve(__dirname, "mini.html"),
        panel: resolve(__dirname, "panel.html"),
      },
    },
  },
});
