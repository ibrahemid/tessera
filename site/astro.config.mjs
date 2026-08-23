import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";
import sitemap from "@astrojs/sitemap";

// spec/vault-format.md opens with its own h1; the page that renders it has one already.
function dropSpecTitle() {
  return (tree, file) => {
    if (!/vault-format\.md$/.test(file.path ?? "")) return;
    const i = tree.children.findIndex((n) => n.type === "element" && n.tagName === "h1");
    if (i >= 0) tree.children.splice(i, 1);
  };
}

export default defineConfig({
  site: "https://tessera.ibrahemid.com",
  output: "static",

  vite: {
    plugins: [tailwindcss()],
  },

  markdown: {
    rehypePlugins: [dropSpecTitle],
  },

  integrations: [
    sitemap({
      filter: (page) => !/\/(og|404)\/?$/.test(new URL(page).pathname),
    }),
  ],
});
