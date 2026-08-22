#!/usr/bin/env node
// Regenerates src/data/cli.json from the real tess binary so /cli never drifts
// from the shipped help text.
//   TESS_BIN=/path/to/tess node scripts/cli-docs.mjs
// Without TESS_BIN it builds the CLI from ../go with `go run`.
import { execFileSync } from "node:child_process";
import { writeFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "..", "src", "data", "cli.json");

class CliDocsError extends Error {
  constructor(message, context) {
    super(message);
    this.name = "CliDocsError";
    this.context = context;
  }
}

function run(args) {
  const bin = process.env.TESS_BIN;
  const cmd = bin ? bin : "go";
  const argv = bin ? args : ["-C", join(here, "..", "..", "go"), "run", "./cmd/tess", ...args];
  try {
    return execFileSync(cmd, argv, { encoding: "utf8", env: { ...process.env, NO_COLOR: "1", TESSERA_VAULT: "/nonexistent/vault.json" } });
  } catch (err) {
    throw new CliDocsError(`tess ${args.join(" ")} failed`, { cmd, argv, stderr: err.stderr });
  }
}

const root = run(["--help"]);
const version = run(["--version"]).trim();
const commandNames = [...root.matchAll(/^  ([a-z]+)\s{2,}/gm)].map((m) => m[1]).filter((n) => n !== "help");

const commands = commandNames.map((name) => {
  const help = run([name, "--help"]);
  const subs = [...help.matchAll(/^  ([a-z]+)\s{2,}/gm)].map((m) => m[1]).filter((n) => n !== "help");
  const subcommands = subs.map((sub) => ({ name: sub, help: run([name, sub, "--help"]) }));
  return { name, help, subcommands };
});

mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, JSON.stringify({ version, generated: new Date().toISOString().slice(0, 10), root, commands }, null, 2) + "\n");
console.log(`wrote ${out}: ${commands.length} commands, ${version}`);
