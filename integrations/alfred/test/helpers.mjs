import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";

const HERE = dirname(fileURLToPath(import.meta.url));
export const WORKFLOW_DIR = join(HERE, "..", "workflow");
export const SCRIPT_PATH = join(WORKFLOW_DIR, "tess.js");
const FIXTURES_DIR = join(HERE, "..", "..", "fixtures");

const EXPORTED_NAMES = [
  "ContractError",
  "AccountMismatchError",
  "TessMissingError",
  "VaultFileSettingError",
  "TessTimeoutError",
  "TessExitError",
  "RequestError",
  "parseListRows",
  "parseCodeRows",
  "parseVaultStatus",
  "accountTitle",
  "accountSubtitle",
  "matchesQuery",
  "accountItem",
  "buildListResponse",
  "buildProblemResponse",
  "firstErrorLine",
  "describeFailure",
  "resolveTess",
  "resolveVaultFile",
  "readActionRequest",
  "codeArguments",
  "selectCodeRow",
  "copiedOutput",
  "commandOutput",
  "pasteOutput",
  "errorOutput",
];

export function loadWorkflow() {
  const context = vm.createContext({});
  vm.runInContext(readFileSync(SCRIPT_PATH, "utf8"), context, { filename: SCRIPT_PATH });
  return vm.runInContext(`({ ${EXPORTED_NAMES.join(", ")} })`, context);
}

export function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

export function fixture(name) {
  return readFileSync(join(FIXTURES_DIR, name), "utf8");
}

export function stderrFixture(key) {
  return JSON.parse(fixture("stderr.json"))[key];
}

export function envFrom(values) {
  return (name) => values[name];
}

const FAKE_SCRIPT = `#!/bin/sh
dir=$(dirname "$0")
{ printf '%s\\n' "$@"; printf '%s\\n' '::end::'; } >> "$dir/calls"
printf '%s\\n' "\${TESSERA_VAULT-::unset::}" >> "$dir/vaults"
case "$1" in
  list) reply=list ;;
  code) reply=code ;;
  vault) reply=status ;;
  *) reply=other ;;
esac
[ -f "$dir/$reply.out" ] && cat "$dir/$reply.out"
[ -f "$dir/$reply.err" ] && cat "$dir/$reply.err" >&2
[ -f "$dir/$reply.sleep" ] && exec sleep "$(cat "$dir/$reply.sleep")"
exit "$(cat "$dir/$reply.exit" 2>/dev/null || echo 0)"
`;

export function createFakeTess(replies) {
  const dir = mkdtempSync(join(tmpdir(), "fake-tess-"));
  const binary = join(dir, "tess");
  writeFileSync(binary, FAKE_SCRIPT);
  chmodSync(binary, 0o755);
  for (const [name, reply] of Object.entries(replies)) {
    if (reply.stdout !== undefined) writeFileSync(join(dir, `${name}.out`), reply.stdout);
    if (reply.stderr !== undefined) writeFileSync(join(dir, `${name}.err`), reply.stderr);
    if (reply.exitCode !== undefined) writeFileSync(join(dir, `${name}.exit`), String(reply.exitCode));
    if (reply.sleepSeconds !== undefined) writeFileSync(join(dir, `${name}.sleep`), String(reply.sleepSeconds));
  }
  return {
    binary,
    dir,
    calls() {
      let text;
      try {
        text = readFileSync(join(dir, "calls"), "utf8");
      } catch {
        return [];
      }
      const calls = [];
      let current = [];
      for (const line of text.split("\n").slice(0, -1)) {
        if (line === "::end::") {
          calls.push(current);
          current = [];
        } else {
          current.push(line);
        }
      }
      return calls;
    },
    vaults() {
      try {
        return readFileSync(join(dir, "vaults"), "utf8").split("\n").slice(0, -1);
      } catch {
        return [];
      }
    },
    remove() {
      rmSync(dir, { recursive: true, force: true });
    },
  };
}

export function createScratchHome() {
  const home = mkdtempSync(join(tmpdir(), "tess-alfred-home-"));
  return {
    env: {
      HOME: home,
      XDG_DATA_HOME: join(home, "xdg"),
      TESSERA_VAULT: join(home, "vault.json"),
    },
    remove() {
      rmSync(home, { recursive: true, force: true });
    },
  };
}

export function runWorkflow(args, env) {
  const result = spawnSync("/usr/bin/osascript", ["-l", "JavaScript", SCRIPT_PATH, ...args], {
    cwd: WORKFLOW_DIR,
    env: { PATH: "/usr/bin:/bin", ...env },
    encoding: "utf8",
    timeout: 30_000,
  });
  if (result.status !== 0) {
    throw new Error(`osascript exited with ${result.status}: ${result.stderr}`);
  }
  return JSON.parse(result.stdout);
}
