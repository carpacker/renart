import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { defineConfig, type Plugin } from "rolldown-vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";

const require = createRequire(import.meta.url);
const PROXY_TARGET = process.env.PROXY_TARGET ?? "http://127.0.0.1:3000"

function prepareMonacoAssetsPlugin(): Plugin {
  let hasPreparedAssets = false;

  const prepareAssets = () => {
    if (hasPreparedAssets) {
      return;
    }

    const loaderPath = require.resolve("monaco-editor/min/vs/loader.js");
    const sourceDir = path.dirname(loaderPath);
    const targetDir = path.resolve(__dirname, "public/monaco/vs");

    fs.rmSync(targetDir, { recursive: true, force: true });
    fs.mkdirSync(path.dirname(targetDir), { recursive: true });
    fs.cpSync(sourceDir, targetDir, { recursive: true, force: true });

    hasPreparedAssets = true;
  };

  return {
    name: "prepare-monaco-assets",
    configResolved() {
      prepareAssets();
    },
  };
}

export default defineConfig({
  plugins: [TanStackRouterVite({ target: "react" }), react(), prepareMonacoAssetsPlugin()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return undefined;
          }

          if (id.includes("reactflow") || id.includes("dagre")) {
            return "graph-vendor";
          }

          if (
            id.includes("@tanstack/react-router") ||
            id.includes("@tanstack/router-core")
          ) {
            return "router-vendor";
          }

          if (id.includes("@tanstack/react-virtual")) {
            return "table-vendor";
          }

          if (
            id.includes("jotai") ||
            id.includes("react-hook-form") ||
            id.includes("@base-ui/react") ||
            id.includes("radix-ui")
          ) {
            return "ui-vendor";
          }

          if (
            id.includes("recharts") ||
            id.includes("victory-vendor") ||
            id.includes("d3-")
          ) {
            return "chart-vendor";
          }

          if (id.includes("@monaco-editor") || id.includes("monaco-editor")) {
            return "monaco-vendor";
          }

          if (
            id.includes("react-markdown") ||
            id.includes("remark-") ||
            id.includes("mdast-util-") ||
            id.includes("micromark") ||
            id.includes("unified")
          ) {
            return "markdown-vendor";
          }

          return undefined;
        },
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "^/api/pipelines/.*/materialize/stream$": {
        target: PROXY_TARGET,
        changeOrigin: true,
        timeout: 0,
        proxyTimeout: 0,
      },
      "^/api/assets/.*/materialize/stream$": {
        target: PROXY_TARGET,
        changeOrigin: true,
        timeout: 0,
        proxyTimeout: 0,
      },
      "/api": {
        target: PROXY_TARGET,
        changeOrigin: true,
        ws: true,
      },
    },
  },
  preview: {
    port: 5173,
  },
});
