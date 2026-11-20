#!/usr/bin/env node
// Regenerates src/data/cli.json from the real tess binary and the README so /cli never
// drifts from the shipped help text, environment variables and vault paths.
//   TESS_BIN=/path/to/tess node scripts/cli-docs.mjs
// Without TESS_BIN it builds the CLI from ../go into a temporary folder.
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..", "..");
const goDir = join(repoRoot, "go");
const out = join(here, "..", "src", "data", "cli.json");

const SAMPLE_PASSPHRASE = "cli-docs-sample-passphrase";
const SAMPLE_URI = "otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub";
const SAMPLE_ARGS = ["code", "github", "--json", "--at", "2026-03-01T09:00:13Z"];

class CliDocsError extends Error {
  constructor(message, context) {
    super(message);
    this.name = "CliDocsError";
    this.context = context;
  }
}

const work = mkdtempSync(join(tmpdir(), "tess-cli-docs-"));

function buildTess() {
  if (process.env.TESS_BIN) return process.env.TESS_BIN;
  const bin = join(work, "tess");
  try {
    execFileSync("go", ["-C", goDir, "build", "-o", bin, "./cmd/tess"], { stdio: ["ignore", "inherit", "inherit"] });
  } catch (err) {
    throw new CliDocsError("go build of ./cmd/tess failed", { goDir, status: err.status });
  }
  return bin;
}

function sandboxEnv(extra = {}) {
  const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("TESSERA_")));
  const home = join(work, "home");
  const data = join(work, "data");
  mkdirSync(home, { recursive: true });
  mkdirSync(data, { recursive: true });
  return { ...env, NO_COLOR: "1", HOME: home, XDG_DATA_HOME: data, TESSERA_VAULT: join(work, "vault.json"), ...extra };
}

function makeRunner(bin) {
  return (args, extra) => {
    try {
      return execFileSync(bin, args, { encoding: "utf8", env: sandboxEnv(extra), stdio: ["ignore", "pipe", "pipe"] });
    } catch (err) {
      throw new CliDocsError(`tess ${args.join(" ")} failed`, { bin, status: err.status, stderr: err.stderr });
    }
  };
}

// Command names live between the Usage and Flags headings. A help text's Long
// description can hold example lines shaped exactly like a command row, so the
// whole text is not safe to scan.
function commandNamesIn(help) {
  const start = help.indexOf("Usage:");
  const end = help.indexOf("\nFlags:", start);
  const section = help.slice(start < 0 ? 0 : start, end < 0 ? help.length : end);
  return [...section.matchAll(/^  ([a-z]+)\s{2,}/gm)].map((m) => m[1]).filter((n) => n !== "help");
}

function readmeSection(readme, heading) {
  const start = readme.indexOf(`\n## ${heading}\n`);
  if (start < 0) throw new CliDocsError(`README.md has no "## ${heading}" section`, { heading });
  const body = readme.slice(start + heading.length + 5);
  const end = body.search(/^## /m);
  return (end < 0 ? body : body.slice(0, end)).trim();
}

function assertPlain(text, where) {
  if (/\[[^\]]*\]\(|\*\*|<[a-z]/i.test(text)) throw new CliDocsError(`README ${where} holds markup /cli cannot render`, { text });
  return text;
}

function paragraphs(text, where) {
  return text.split(/\n{2,}/).map((p) => assertPlain(p.replace(/\n/g, " ").trim(), where)).filter(Boolean);
}

function parseEnvironment(readme) {
  const section = readmeSection(readme, "Environment");
  const lines = section.split("\n");
  const rows = lines.filter((l) => /^\| `[A-Z_]+` \|/.test(l));
  const variables = rows.map((l) => {
    const m = l.match(/^\| `([A-Z_]+)` \| (.+?) \|$/);
    if (!m) throw new CliDocsError("README environment row has an unexpected shape", { line: l });
    return { name: m[1], effect: assertPlain(m[2], "environment row") };
  });
  if (variables.length === 0) throw new CliDocsError("README environment table is empty", {});
  const afterTable = lines.slice(lines.findLastIndex((l) => l.startsWith("|")) + 1).join("\n");
  return { variables, notes: paragraphs(afterTable, "environment notes") };
}

function parseVaultLocation(readme) {
  const section = readmeSection(readme, "Vault location");
  const blocks = section.split(/\n{2,}/);
  const listIndex = blocks.findIndex((b) => b.startsWith("- "));
  if (listIndex < 1) throw new CliDocsError("README vault location has no lead and platform list", {});
  const platforms = blocks[listIndex].split("\n").map((l) => {
    const m = l.match(/^- ([^:]+): (.+)$/);
    if (!m) throw new CliDocsError("README vault location bullet has an unexpected shape", { line: l });
    return { os: m[1], path: assertPlain(m[2], "vault location bullet") };
  });
  return {
    lead: paragraphs(blocks.slice(0, listIndex).join("\n\n"), "vault location lead").join(" "),
    platforms,
    notes: paragraphs(blocks.slice(listIndex + 1).join("\n\n"), "vault location notes"),
  };
}

function goSources(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return name === "testdata" ? [] : goSources(p);
    return name.endsWith(".go") && !name.endsWith("_test.go") ? [p] : [];
  });
}

function checkEnvironment(variables) {
  const source = goSources(goDir).map((p) => readFileSync(p, "utf8")).join("\n");
  const read = new Set([...source.matchAll(/"(TESSERA_[A-Z_]+)"/g)].map((m) => m[1]));
  const documented = new Set(variables.map((v) => v.name));
  const undocumented = [...read].filter((n) => !documented.has(n));
  const unread = [...documented].filter((n) => !source.includes(`"${n}"`));
  if (undocumented.length || unread.length) {
    throw new CliDocsError("README environment table and the CLI source disagree", { undocumented, unread });
  }
}

function jsonSample(run) {
  const env = { TESSERA_PASSPHRASE: SAMPLE_PASSPHRASE };
  run(["vault", "init"], env);
  run(["add", SAMPLE_URI], env);
  const output = run(SAMPLE_ARGS, env).trim();
  JSON.parse(output);
  return { command: `tess ${SAMPLE_ARGS.join(" ")}`, output };
}

try {
  const run = makeRunner(buildTess());
  const readme = readFileSync(join(repoRoot, "README.md"), "utf8");
  const environment = parseEnvironment(readme);
  checkEnvironment(environment.variables);
  const vaultLocation = parseVaultLocation(readme);

  const root = run(["--help"]);
  const version = run(["--version"]).trim();
  const commands = commandNamesIn(root).map((name) => {
    const help = run([name, "--help"]);
    const subcommands = commandNamesIn(help).map((sub) => ({ name: sub, help: run([name, sub, "--help"]) }));
    return { name, help, subcommands };
  });
  const sample = jsonSample(run);

  mkdirSync(dirname(out), { recursive: true });
  const data = { version, generated: new Date().toISOString().slice(0, 10), root, commands, environment, vaultLocation, jsonSample: sample };
  writeFileSync(out, JSON.stringify(data, null, 2) + "\n");
  console.log(`wrote ${out}: ${commands.length} commands, ${environment.variables.length} variables, ${version}`);
} finally {
  rmSync(work, { recursive: true, force: true });
}
