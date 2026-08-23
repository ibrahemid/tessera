#!/usr/bin/env node
// Renders every page of a built dist at 1440, 1920, 2560 and 390 and records
// document.scrollingElement.scrollWidth against innerWidth, the hero media box
// and the header inner box so their inline edges can be compared.
//   node scripts/render-widths.mjs dist --out <dir> [--page /path ...] [--fold] [--dark]
import { createServer } from "node:http";
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { extname, join, resolve } from "node:path";
import { homedir } from "node:os";

class RenderWidthsError extends Error {
  constructor(message, context = {}) {
    super(message);
    this.name = "RenderWidthsError";
    this.context = context;
  }
}

const MIME = {
  ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png",
  ".jpg": "image/jpeg", ".webp": "image/webp", ".avif": "image/avif", ".woff2": "font/woff2", ".woff": "font/woff",
  ".mp4": "video/mp4", ".webm": "video/webm", ".txt": "text/plain; charset=utf-8", ".xml": "application/xml",
};

const WIDTHS = [
  { width: 1440, height: 900 },
  { width: 1920, height: 1080 },
  { width: 2560, height: 1440 },
  { width: 390, height: 844 },
];

const PAGES = ["/", "/cli", "/security", "/vault-format", "/vs/ente", "/vs/authy", "/changelog", "/press", "/learn", "/privacy", "/support"];

function serveDir(root) {
  const server = createServer((req, res) => {
    const url = decodeURIComponent((req.url || "/").split("?")[0]);
    let path = resolve(join(root, url));
    if (!path.startsWith(resolve(root))) { res.writeHead(403).end(); return; }
    try { if (statSync(path).isDirectory()) path = join(path, "index.html"); } catch { if (existsSync(path + ".html")) path += ".html"; }
    try {
      res.writeHead(200, { "content-type": MIME[extname(path).toLowerCase()] || "application/octet-stream", "cache-control": "no-store" });
      res.end(readFileSync(path));
    } catch { res.writeHead(404).end(); }
  });
  return new Promise((ok) => server.listen(0, "127.0.0.1", () => ok({ server, port: server.address().port })));
}

async function loadChromium() {
  const candidates = [
    process.env.PLAYWRIGHT_MODULE,
    "playwright",
    join(homedir(), "Desktop/Pode/wakeproof/site/node_modules/playwright/index.mjs"),
  ].filter(Boolean);
  for (const spec of candidates) {
    if (spec.startsWith("/") && !existsSync(spec)) continue;
    try { return (await import(spec)).chromium; } catch { /* next */ }
  }
  throw new RenderWidthsError("no playwright install found", { candidates });
}

const CHECKS = () => {
  const box = (sel) => {
    const el = document.querySelector(sel);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { left: Math.round(r.left), right: Math.round(r.right), width: Math.round(r.width), height: Math.round(r.height) };
  };
  const widest = [...document.body.querySelectorAll("*")].reduce((acc, el) => {
    const r = el.getBoundingClientRect();
    return r.right > acc.right ? { right: Math.round(r.right), tag: el.tagName.toLowerCase() + (el.className && typeof el.className === "string" ? "." + el.className.split(" ")[0] : "") } : acc;
  }, { right: 0, tag: "" });
  return {
    innerWidth: window.innerWidth,
    scrollWidth: document.scrollingElement.scrollWidth,
    header: box("[data-grid-ref]"),
    hero: box("[data-hero]"),
    widest,
  };
};

async function main() {
  const args = process.argv.slice(2);
  let target = null, out = null, fold = false, dark = false;
  const pages = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--out") out = args[++i];
    else if (args[i] === "--page") pages.push(args[++i]);
    else if (args[i] === "--fold") fold = true;
    else if (args[i] === "--dark") dark = true;
    else target = args[i];
  }
  if (!target) throw new RenderWidthsError("usage: render-widths.mjs <dist> --out <dir>");
  const root = resolve(target);
  const dir = resolve(out || join(root, "..", "design", "renders", "widths"));
  mkdirSync(dir, { recursive: true });
  const { server, port } = await serveDir(root);
  const chromium = await loadChromium();
  const browser = await chromium.launch();
  const lines = [];
  let failed = 0;
  try {
    for (const p of pages.length ? pages : PAGES) {
      for (const v of WIDTHS) {
        const page = await browser.newPage({ viewport: v, deviceScaleFactor: v.width >= 1920 ? 1 : 2, colorScheme: dark ? "dark" : "light" });
        try {
          await page.goto(`http://127.0.0.1:${port}${p}`, { waitUntil: "networkidle", timeout: 30000 });
          await page.evaluate(async () => { await document.fonts.ready; });
          await page.waitForTimeout(300);
          const slug = p.replace(/^\/|\/$/g, "").replace(/[^\w.-]+/g, "-") || "index";
          await page.screenshot({ path: join(dir, `${slug}-${v.width}-full.png`), fullPage: true });
          if (fold || v.width === 1440) await page.screenshot({ path: join(dir, `${slug}-${v.width}-fold.png`), fullPage: false });
          const c = await page.evaluate(CHECKS);
          const over = c.scrollWidth > c.innerWidth ? "  OVERFLOW" : "";
          lines.push(`${p} @${v.width}: scrollWidth ${c.scrollWidth} / inner ${c.innerWidth}${over}; widest right ${c.widest.right} (${c.widest.tag})` +
            (c.header ? `; header ${c.header.left}..${c.header.right}` : "") +
            (c.hero ? `; hero ${c.hero.left}..${c.hero.right} ${c.hero.width}x${c.hero.height}` : ""));
          if (over) failed++;
        } catch (err) {
          failed++;
          lines.push(`${p} @${v.width}: ERROR ${err.message.split("\n")[0]}`);
        } finally { await page.close(); }
      }
    }
  } finally {
    await browser.close();
    await new Promise((ok) => server.close(ok));
  }
  writeFileSync(join(dir, "checks.txt"), lines.join("\n") + "\n");
  process.stdout.write(lines.join("\n") + "\n" + join(dir, "checks.txt") + "\n");
  process.exit(failed ? 1 : 0);
}

main().catch((err) => {
  process.stderr.write(`render-widths: ${err.message}${err.context ? " " + JSON.stringify(err.context) : ""}\n`);
  process.exit(1);
});
