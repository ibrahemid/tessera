import { describe, expect, it } from "vitest";
import { AccountMismatchError } from "../src/lib/accounts";
import { ContractError, parseVaultStatus } from "../src/lib/contract";
import { TessExitError, TessMissingError, TessTimeoutError, VaultFileSettingError } from "../src/lib/errors";
import { describeFailure, firstErrorLine } from "../src/lib/problems";
import { fixture, stderrFixture } from "./helpers";

const locked = new TessExitError(["list", "--json"], 1, stderrFixture("locked"));

describe("describeFailure", () => {
  it("names a configured path that does not run", () => {
    const problem = describeFailure(new TessMissingError("/opt/tess"), undefined, false);
    expect(problem.kind).toBe("missing");
    expect(problem.message).toBe("Nothing runnable at /opt/tess. Fix the path in the extension preferences.");
  });

  it("names a vault file setting that is not a full path", () => {
    const problem = describeFailure(new VaultFileSettingError("vault.json"), undefined, false);
    expect(problem.kind).toBe("vault-file");
    expect(problem.message).toBe("vault.json is not a full path. Fix the vault file in the extension preferences.");
  });

  it("reports a timeout", () => {
    const problem = describeFailure(new TessTimeoutError(["list", "--json"], 15_000), undefined, false);
    expect(problem).toEqual({
      kind: "timeout",
      title: "tess did not respond",
      message: "tess list did not finish within 15 seconds.",
      command: undefined,
    });
  });

  it("prefers a missing vault over a missing passphrase", () => {
    const status = parseVaultStatus(fixture("status-missing.json"));
    const problem = describeFailure(new TessExitError(["list"], 1, stderrFixture("no_vault")), status, false);
    expect(problem.kind).toBe("no-vault");
    expect(problem.message).toBe(
      "There is no vault at /Users/me/.local/share/tessera/vault.json. Create one with tess vault init in a terminal, or set the vault file in the extension preferences.",
    );
  });

  it("uses tess's error without a vault status", () => {
    expect(describeFailure(locked, undefined, false).message).toBe(
      "set $TESSERA_PASSPHRASE or run on a terminal to enter the vault passphrase",
    );
  });

  it("uses the exit status when tess printed nothing", () => {
    expect(describeFailure(new TessExitError(["list"], 2, ""), undefined, false).message).toBe(
      "tess list exited with status 2.",
    );
  });

  it("reports a changed account", () => {
    const problem = describeFailure(new AccountMismatchError("gone", "id"), undefined, false);
    expect(problem.kind).toBe("account-changed");
  });

  it("reports output that breaks the contract", () => {
    const problem = describeFailure(new ContractError("list row 1: `id` is missing"), undefined, false);
    expect(problem.kind).toBe("unexpected-output");
    expect(problem.message).toBe("list row 1: `id` is missing.");
  });
});

describe("firstErrorLine", () => {
  it("drops the error prefix and later lines", () => {
    expect(firstErrorLine("\nerror: wrong passphrase\nhint: tess vault restore --bak\n")).toBe("wrong passphrase");
  });
});
