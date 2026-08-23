#!/usr/bin/env node
// Checks a built dist: one h1 per page, meta description length, internal links and
// anchors resolve, og:image exists. Exit 1 on any finding.
//   node scripts/check-dist.mjs dist
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const dist = resolve(process.argv[2] ?? "dist");
const MAX_DESCRIPTION = 160;

class DistCheckError extends Error {
  constructor(message, context) {
    super(message);
    this.name = "DistCheckError";
    this.context = context;
  }
}

if (!existsSync(dist)) throw new DistCheckError("dist directory missing", { dist });

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : p.endsWith(".html") ? [p] : [];
  });
}

const decode = (s) => s.replace(/&amp;/g, "&").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">");
const pages = walk(dist).filter((p) => !/\/og\/index\.html$/.test(p));
const ids = new Map(pages.map((p) => [p, new Set([...readFileSync(p, "utf8").matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]))]));

function targetFile(path) {
  const clean = path.replace(/\/$/, "");
  const candidates = [join(dist, clean, "index.html"), join(dist, clean), join(dist, `${clean}.html`)];
  return candidates.find((c) => existsSync(c) && statSync(c).isFile());
}

const findings = [];
for (const page of pages) {
  const html = readFileSync(page, "utf8");
  const name = relative(dist, page);
  const h1s = (html.match(/<h1[\s>]/g) ?? []).length;
  if (h1s !== 1) findings.push(`${name}: ${h1s} h1 elements`);
  const desc = html.match(/<meta name="description" content="([^"]*)"/);
  if (!desc) findings.push(`${name}: no meta description`);
  else if (decode(desc[1]).length > MAX_DESCRIPTION) findings.push(`${name}: description is ${decode(desc[1]).length} chars`);
  const og = html.match(/<meta property="og:image" content="([^"]*)"/);
  if (og && !existsSync(join(dist, new URL(og[1]).pathname))) findings.push(`${name}: og:image missing from dist`);
  for (const m of html.matchAll(/href="([^"]+)"/g)) {
    const href = decode(m[1]);
    if (!href.startsWith("/") || href.startsWith("//")) continue;
    const [path, anchor] = href.split("#");
    const file = path ? targetFile(path) : page;
    if (!file) { findings.push(`${name}: dead link ${href}`); continue; }
    if (anchor && !ids.get(file)?.has(anchor)) findings.push(`${name}: missing anchor ${href}`);
  }
}

if (findings.length) {
  console.error(findings.join("\n"));
  process.exit(1);
}
console.log(`checked ${pages.length} pages`);
