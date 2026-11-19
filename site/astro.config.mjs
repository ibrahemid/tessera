import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";
import sitemap from "@astrojs/sitemap";
import { satteri } from "@astrojs/markdown-satteri";
import { defineHastPlugin } from "satteri";

// spec/vault-format.md opens with its own h1; the page that renders it has one already.
function dropSpecTitle(ctx) {
  if (!/\/vault-format\.md$/.test(ctx.fileURL?.pathname ?? "")) return null;
  let dropped = false;
  return defineHastPlugin({
    name: "drop-spec-title",
    element: {
      filter: ["h1"],
      visit(node, visit) {
        if (dropped) return;
        dropped = true;
        visit.removeNode(node);
      },
    },
  });
}

export default defineConfig({
  site: "https://tessera.ibrahemid.com",
  output: "static",

  vite: {
    plugins: [tailwindcss()],
  },

  markdown: {
    processor: satteri({ hastPlugins: [dropSpecTitle] }),
    shikiConfig: { theme: "css-variables" },
  },

  integrations: [
    sitemap({
      filter: (page) => !/\/(og|404)\/?$/.test(new URL(page).pathname),
    }),
  ],
});
