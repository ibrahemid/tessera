import { AccountMismatchError } from "./accounts";
import { ContractError, VaultStatus } from "./contract";
import { TessExitError, TessMissingError, TessTimeoutError, VaultFileSettingError } from "./errors";

export type ProblemKind =
  | "missing"
  | "vault-file"
  | "timeout"
  | "no-vault"
  | "unreadable"
  | "store-locked"
  | "not-remembered"
  | "account-changed"
  | "unexpected-output"
  | "failed";

export interface Problem {
  kind: ProblemKind;
  title: string;
  message: string;
  command: string | undefined;
}

function problem(kind: ProblemKind, title: string, message: string, command?: string): Problem {
  return { kind, title, message, command };
}

export function firstErrorLine(stderr: string): string {
  const line = stderr
    .split("\n")
    .map((text) => text.trim())
    .find((text) => text !== "");
  return line === undefined ? "" : line.replace(/^error:\s*/, "");
}

function describeExit(error: TessExitError, status: VaultStatus | undefined, passphraseFromEnv: boolean): Problem {
  if (status !== undefined) {
    if (!status.exists) {
      return problem(
        "no-vault",
        "No vault",
        `There is no vault at ${status.path}. Create one with tess vault init in a terminal, or set the vault file in the extension preferences.`,
        "tess vault init",
      );
    }
    if (status.unreadable !== undefined) {
      return problem("unreadable", "Vault unreadable", status.unreadable);
    }
    if (status.lockedIn.length > 0) {
      return problem("store-locked", "Vault locked", `Unlock the ${status.lockedIn.join(" and ")}, then try again.`);
    }
    if (status.rememberedIn.length === 0 && !passphraseFromEnv) {
      return problem(
        "not-remembered",
        "Vault locked",
        "Run tess vault remember in a terminal so tess can unlock the vault without a prompt, then try again.",
        "tess vault remember",
      );
    }
  }
  const detail = firstErrorLine(error.stderr);
  return problem("failed", "tess failed", detail === "" ? `${error.message}.` : detail);
}

export function describeFailure(error: unknown, status: VaultStatus | undefined, passphraseFromEnv: boolean): Problem {
  if (error instanceof TessMissingError) {
    return error.configuredPath === undefined
      ? problem("missing", "tess not found", "Install tess, or set its path in the extension preferences.")
      : problem(
          "missing",
          "tess not found",
          `Nothing runnable at ${error.configuredPath}. Fix the path in the extension preferences.`,
        );
  }
  if (error instanceof VaultFileSettingError) {
    return problem(
      "vault-file",
      "Vault file not usable",
      `${error.configuredPath} is not a full path. Fix the vault file in the extension preferences.`,
    );
  }
  if (error instanceof TessTimeoutError) {
    return problem("timeout", "tess did not respond", `${error.message}.`);
  }
  if (error instanceof TessExitError) {
    return describeExit(error, status, passphraseFromEnv);
  }
  if (error instanceof AccountMismatchError) {
    return problem("account-changed", "Could not get the code", error.message);
  }
  if (error instanceof ContractError) {
    return problem("unexpected-output", "Unexpected output from tess", `${error.message}.`);
  }
  return problem("failed", "tess failed", error instanceof Error ? error.message : String(error));
}
