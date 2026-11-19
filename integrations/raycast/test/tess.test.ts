import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { AccountMismatchError } from "../src/lib/accounts";
import { ContractError, parseListRows } from "../src/lib/contract";
import { TessExitError, TessMissingError, TessTimeoutError, VaultFileSettingError } from "../src/lib/errors";
import { TessClient, candidatePaths, explainFailure, locateTess, runTess, tessEnvironment } from "../src/lib/tess";
import { FakeReply, FakeTess, createFakeTess, fixture, stderrFixture } from "./helpers";

const rows = parseListRows(fixture("list.json"));
const row = (handle: string) => rows.find((candidate) => candidate.handle === handle)!;
const NO_PASSPHRASE: NodeJS.ProcessEnv = { PATH: "/usr/bin:/bin" };

let fakes: FakeTess[] = [];
let dirs: string[] = [];

function fake(replies: { list?: FakeReply; code?: FakeReply; status?: FakeReply }): FakeTess {
  const created = createFakeTess(replies);
  fakes.push(created);
  return created;
}

function tempDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "tess-home-"));
  dirs.push(dir);
  return dir;
}

afterEach(() => {
  fakes.forEach((created) => created.remove());
  dirs.forEach((dir) => rmSync(dir, { recursive: true, force: true }));
  fakes = [];
  dirs = [];
});

describe("TessClient", () => {
  it("lists accounts with an argument array", async () => {
    const tess = fake({ list: { stdout: fixture("list.json") } });
    const accounts = await new TessClient(tess.binary).listAccounts();
    expect(accounts.map((account) => account.handle)).toEqual(["gi", "ac", "ba", "ex", "st"]);
    expect(tess.calls()).toEqual([["list", "--json"]]);
  });

  it("asks for one code by id after the end of options", async () => {
    const tess = fake({ code: { stdout: fixture("code-totp.json") } });
    const code = await new TessClient(tess.binary).getCode(row("gi").id);
    expect(code.code).toBe("219515");
    expect(tess.calls()).toEqual([["code", "--json", "--", row("gi").id]]);
  });

  it("has tess copy the code and clear it after 30 seconds", async () => {
    const tess = fake({ code: { stdout: fixture("code-totp.json"), stderr: "copied\n" } });
    await new TessClient(tess.binary).copyCode(row("gi").id, false);
    expect(tess.calls()).toEqual([["code", "-c", "--clear", "30", "--json", "--", row("gi").id]]);
  });

  it("has tess copy the next code", async () => {
    const tess = fake({ code: { stdout: fixture("code-hotp.json") } });
    await new TessClient(tess.binary).copyCode(row("ba").id, true);
    expect(tess.calls()).toEqual([["code", "-c", "--clear", "30", "--json", "--next", "--", row("ba").id]]);
  });

  it("refuses a code for another account", async () => {
    const tess = fake({ code: { stdout: fixture("code-totp.json") } });
    await expect(new TessClient(tess.binary).getCode(row("ac").id)).rejects.toThrow(AccountMismatchError);
    await expect(new TessClient(tess.binary).copyCode(row("ac").id, false)).rejects.toThrow(AccountMismatchError);
  });

  it("refuses several rows", async () => {
    const tess = fake({ code: { stdout: fixture("code-ambiguous.json") } });
    await expect(new TessClient(tess.binary).getCode(row("ex").id)).rejects.toThrow(AccountMismatchError);
  });

  it("passes the vault file to tess", async () => {
    const tess = fake({ list: { stdout: fixture("list.json") } });
    const env = tessEnvironment("~/vaults/work.json", "/Users/me", { PATH: "/usr/bin:/bin" });
    await new TessClient(tess.binary, { env }).listAccounts();
    expect(tess.vaults()).toEqual(["/Users/me/vaults/work.json"]);
  });

  it("reports a non-zero exit with its stderr", async () => {
    const tess = fake({ list: { stderr: stderrFixture("locked"), exitCode: 1 } });
    const failure = await new TessClient(tess.binary).listAccounts().catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(TessExitError);
    expect((failure as TessExitError).exitCode).toBe(1);
    expect((failure as TessExitError).stderr).toBe(stderrFixture("locked"));
  });

  it("reports output that breaks the contract", async () => {
    const tess = fake({ list: { stdout: "GitHub alice@example.com\n" } });
    await expect(new TessClient(tess.binary).listAccounts()).rejects.toThrow(ContractError);
  });
});

describe("runTess", () => {
  it("reports a missing binary", async () => {
    await expect(runTess(join(tempDir(), "tess"), ["list", "--json"])).rejects.toThrow(TessMissingError);
  });

  it("stops a tess that does not finish", async () => {
    const tess = fake({ list: { sleepSeconds: 5 } });
    const started = Date.now();
    await expect(runTess(tess.binary, ["list", "--json"], { timeoutMs: 300 })).rejects.toThrow(TessTimeoutError);
    expect(Date.now() - started).toBeLessThan(3000);
  });
});

describe("locateTess", () => {
  it("searches the usual install paths in order", () => {
    expect(candidatePaths("/Users/me")).toEqual([
      "/opt/homebrew/bin/tess",
      "/usr/local/bin/tess",
      "/Users/me/.local/bin/tess",
      "/Users/me/go/bin/tess",
    ]);
  });

  it("uses the configured path, expanding the home directory", async () => {
    const home = tempDir();
    mkdirSync(join(home, "bin"));
    writeFileSync(join(home, "bin", "tess"), "#!/bin/sh\n");
    chmodSync(join(home, "bin", "tess"), 0o755);
    await expect(locateTess(" ~/bin/tess ", home)).resolves.toBe(join(home, "bin", "tess"));
  });

  it("refuses a configured path that is missing, relative, a directory or not executable", async () => {
    const home = tempDir();
    writeFileSync(join(home, "tess"), "#!/bin/sh\n");
    chmodSync(join(home, "tess"), 0o644);
    await expect(locateTess(join(home, "missing"), home)).rejects.toThrow(TessMissingError);
    await expect(locateTess("bin/tess", home)).rejects.toThrow(TessMissingError);
    await expect(locateTess(home, home)).rejects.toThrow(TessMissingError);
    await expect(locateTess(join(home, "tess"), home)).rejects.toThrow(TessMissingError);
  });
});

describe("tessEnvironment", () => {
  const base = { PATH: "/usr/bin:/bin", TESSERA_VAULT: "/elsewhere/vault.json" };

  it("keeps the environment when no vault file is set", () => {
    expect(tessEnvironment(undefined, "/Users/me", base)).toBe(base);
    expect(tessEnvironment("  ", "/Users/me", base)).toBe(base);
  });

  it("sets TESSERA_VAULT, expanding the home directory", () => {
    expect(tessEnvironment(" ~/vault.json ", "/Users/me", base)).toEqual({
      PATH: "/usr/bin:/bin",
      TESSERA_VAULT: "/Users/me/vault.json",
    });
    expect(tessEnvironment("/Volumes/keys/vault.json", "/Users/me", base).TESSERA_VAULT).toBe(
      "/Volumes/keys/vault.json",
    );
  });

  it("refuses a relative vault file", () => {
    expect(() => tessEnvironment("vault.json", "/Users/me", base)).toThrow(VaultFileSettingError);
  });
});

describe("explainFailure", () => {
  async function explainListFailure(status: FakeReply | undefined, env: NodeJS.ProcessEnv = NO_PASSPHRASE) {
    const tess = fake({ list: { stderr: stderrFixture("locked"), exitCode: 1 }, status });
    const client = new TessClient(tess.binary);
    const failure = await client.listAccounts().catch((error: unknown) => error);
    return { problem: await explainFailure(failure, client, env), calls: tess.calls() };
  }

  it("tells the user to remember the passphrase when the vault is locked", async () => {
    const { problem, calls } = await explainListFailure({ stdout: fixture("status-not-remembered.json") });
    expect(problem.kind).toBe("not-remembered");
    expect(problem.command).toBe("tess vault remember");
    expect(calls).toEqual([
      ["list", "--json"],
      ["vault", "status", "--json"],
    ]);
  });

  it("shows tess's error when the passphrase comes from the environment", async () => {
    const { problem } = await explainListFailure(
      { stdout: fixture("status-not-remembered.json") },
      { TESSERA_PASSPHRASE: "x" },
    );
    expect(problem.kind).toBe("failed");
    expect(problem.message).toBe("set $TESSERA_PASSPHRASE or run on a terminal to enter the vault passphrase");
  });

  it("asks to unlock a locked secret store", async () => {
    const { problem } = await explainListFailure({ stdout: fixture("status-store-locked.json") });
    expect(problem.kind).toBe("store-locked");
    expect(problem.message).toBe("Unlock the login keychain, then try again.");
  });

  it("points at tess vault init when there is no vault", async () => {
    const { problem } = await explainListFailure({ stdout: fixture("status-missing.json") });
    expect(problem.kind).toBe("no-vault");
    expect(problem.command).toBe("tess vault init");
  });

  it("shows why the vault is unreadable", async () => {
    const { problem } = await explainListFailure({ stdout: fixture("status-unreadable.json") });
    expect(problem.kind).toBe("unreadable");
    expect(problem.message).toMatch(/permission denied/);
  });

  it("falls back to tess's error when the status cannot be read", async () => {
    const { problem } = await explainListFailure({ stderr: "error: unknown command\n", exitCode: 1 });
    expect(problem.kind).toBe("failed");
    expect(problem.message).toBe("set $TESSERA_PASSPHRASE or run on a terminal to enter the vault passphrase");
  });

  it("shows tess's error when the passphrase is remembered", async () => {
    const tess = fake({
      code: { stderr: stderrFixture("no_match"), exitCode: 1 },
      status: { stdout: fixture("status-remembered.json") },
    });
    const client = new TessClient(tess.binary);
    const failure = await client.getCode(row("gi").id).catch((error: unknown) => error);
    const problem = await explainFailure(failure, client, NO_PASSPHRASE);
    expect(problem).toEqual({
      kind: "failed",
      title: "tess failed",
      message: 'no account matches "gi"',
      command: undefined,
    });
  });

  it("explains a missing tess without running anything", async () => {
    const failure = await locateTess(join(tempDir(), "tess")).catch((error: unknown) => error);
    const problem = await explainFailure(failure, undefined, NO_PASSPHRASE);
    expect(problem.kind).toBe("missing");
    expect(problem.title).toBe("tess not found");
  });
});
