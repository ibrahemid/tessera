import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  site: "https://tessera.ibrahemid.com",
  output: "static",
  vite: {
    plugins: [tailwindcss()],
  },
});
