#!/usr/bin/env node
// Renders the built /og page to public/og.png (1200x630, under 300 KB).
//   npm run build && PLAYWRIGHT=/path/to/playwright/index.mjs node scripts/og.mjs
import { createServer } from "node:http";
import { existsSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const here = dirname(fileURLToPath(import.meta.url));
const dist = resolve(here, "..", "dist");
const out = resolve(here, "..", "public", "og.png");
const LIMIT = 300 * 1024;

class OgRenderError extends Error {
  constructor(message, context) {
    super(message);
    this.name = "OgRenderError";
    this.context = context;
  }
}

const types = {
  ".html": "text/html", ".css": "text/css", ".js": "text/javascript", ".webp": "image/webp",
  ".png": "image/png", ".svg": "image/svg+xml", ".woff2": "font/woff2", ".woff": "font/woff",
};

async function loadPlaywright() {
  const candidates = [process.env.PLAYWRIGHT, "playwright"].filter(Boolean);
  for (const c of candidates) {
    try { return await import(c); } catch (_) {}
  }
  throw new OgRenderError("playwright not found; set PLAYWRIGHT to its index.mjs", { candidates });
}

if (!existsSync(join(dist, "og", "index.html"))) {
  throw new OgRenderError("dist/og/index.html missing; run npm run build first", { dist });
}

const server = createServer((req, res) => {
  let file = join(dist, decodeURIComponent(new URL(req.url, "http://x").pathname));
  if (existsSync(file) && statSync(file).isDirectory()) file = join(file, "index.html");
  if (!existsSync(file)) { res.writeHead(404); res.end(); return; }
  res.writeHead(200, { "content-type": types[extname(file)] ?? "application/octet-stream" });
  res.end(readFileSync(file));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const { port } = server.address();

const { chromium } = await loadPlaywright();
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
  await page.goto(`http://127.0.0.1:${port}/og/`, { waitUntil: "networkidle" });
  await page.evaluate(() => document.fonts.ready);
  const shot = await page.screenshot({ type: "png", clip: { x: 0, y: 0, width: 1200, height: 630 } });
  const png = await sharp(shot).png({ compressionLevel: 9, palette: true, quality: 92 }).toBuffer();
  if (png.length > LIMIT) throw new OgRenderError(`og.png is ${png.length} bytes, over ${LIMIT}`, { out });
  writeFileSync(out, png);
  console.log(`wrote ${out} (${png.length} bytes)`);
} finally {
  await browser.close();
  server.close();
}
