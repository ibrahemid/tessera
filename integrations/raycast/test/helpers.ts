import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const FIXTURES_DIR = join(__dirname, "..", "..", "fixtures");

export function fixture(name: string): string {
  return readFileSync(join(FIXTURES_DIR, name), "utf8");
}

export function stderrFixture(key: "locked" | "no_vault" | "no_match"): string {
  const all = JSON.parse(fixture("stderr.json")) as Record<string, string>;
  return all[key];
}

export interface FakeReply {
  stdout?: string;
  stderr?: string;
  exitCode?: number;
  sleepSeconds?: number;
}

export interface FakeTess {
  binary: string;
  calls(): string[][];
  vaults(): string[];
  remove(): void;
}

const SCRIPT = `#!/bin/sh
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

export function createFakeTess(replies: { list?: FakeReply; code?: FakeReply; status?: FakeReply }): FakeTess {
  const dir = mkdtempSync(join(tmpdir(), "fake-tess-"));
  const binary = join(dir, "tess");
  writeFileSync(binary, SCRIPT);
  chmodSync(binary, 0o755);
  for (const [name, reply] of Object.entries(replies)) {
    if (reply.stdout !== undefined) writeFileSync(join(dir, `${name}.out`), reply.stdout);
    if (reply.stderr !== undefined) writeFileSync(join(dir, `${name}.err`), reply.stderr);
    if (reply.exitCode !== undefined) writeFileSync(join(dir, `${name}.exit`), String(reply.exitCode));
    if (reply.sleepSeconds !== undefined) writeFileSync(join(dir, `${name}.sleep`), String(reply.sleepSeconds));
  }
  return {
    binary,
    calls() {
      let text: string;
      try {
        text = readFileSync(join(dir, "calls"), "utf8");
      } catch {
        return [];
      }
      const calls: string[][] = [];
      let current: string[] = [];
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
