import { ExecFileException, execFile } from "node:child_process";
import { constants } from "node:fs";
import { access, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { selectCodeRow } from "./accounts";
import {
  CodeRow,
  ContractError,
  ListRow,
  VaultStatus,
  parseCodeRows,
  parseListRows,
  parseVaultStatus,
} from "./contract";
import { TessExitError, TessMissingError, TessTimeoutError, VaultFileSettingError } from "./errors";
import { Problem, describeFailure } from "./problems";

export const TESS_TIMEOUT_MS = 15_000;
const CLEAR_AFTER_SECONDS = 30;
const MAX_OUTPUT_BYTES = 8 * 1024 * 1024;

export interface RunOptions {
  timeoutMs?: number;
  env?: NodeJS.ProcessEnv;
}

export function candidatePaths(home: string): string[] {
  return ["/opt/homebrew/bin/tess", "/usr/local/bin/tess", join(home, ".local/bin/tess"), join(home, "go/bin/tess")];
}

function expandHome(path: string, home: string): string {
  if (path === "~") {
    return home;
  }
  return path.startsWith("~/") ? join(home, path.slice(2)) : path;
}

async function isExecutableFile(path: string): Promise<boolean> {
  try {
    const info = await stat(path);
    if (!info.isFile()) {
      return false;
    }
    await access(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

export function tessEnvironment(
  vaultFile: string | undefined,
  home: string = homedir(),
  base: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  const configured = vaultFile?.trim() ?? "";
  if (configured === "") {
    return base;
  }
  const path = expandHome(configured, home);
  if (!isAbsolute(path)) {
    throw new VaultFileSettingError(configured);
  }
  return { ...base, TESSERA_VAULT: path };
}

function copyArguments(accountId: string, next: boolean): string[] {
  return ["code", "-c", "--clear", String(CLEAR_AFTER_SECONDS), "--json", ...(next ? ["--next"] : []), "--", accountId];
}

function codeArguments(accountId: string): string[] {
  return ["code", "--json", "--", accountId];
}

export async function locateTess(configuredPath: string | undefined, home: string = homedir()): Promise<string> {
  const configured = configuredPath?.trim() ?? "";
  if (configured !== "") {
    const path = expandHome(configured, home);
    if (isAbsolute(path) && (await isExecutableFile(path))) {
      return path;
    }
    throw new TessMissingError(path);
  }
  for (const path of candidatePaths(home)) {
    if (await isExecutableFile(path)) {
      return path;
    }
  }
  throw new TessMissingError(undefined);
}

function toRunError(
  error: ExecFileException,
  binary: string,
  args: string[],
  stderr: string,
  timeoutMs: number,
): Error {
  if (error.code === "ENOENT" || error.code === "EACCES") {
    return new TessMissingError(binary);
  }
  if (error.code === "ERR_CHILD_PROCESS_STDIO_MAXBUFFER") {
    return new ContractError(`tess ${args[0]} printed more than ${MAX_OUTPUT_BYTES} bytes`);
  }
  if (error.killed === true && error.signal !== undefined && error.signal !== null) {
    return new TessTimeoutError(args, timeoutMs);
  }
  return new TessExitError(args, typeof error.code === "number" ? error.code : null, stderr);
}

export function runTess(binary: string, args: string[], options: RunOptions = {}): Promise<string> {
  const timeoutMs = options.timeoutMs ?? TESS_TIMEOUT_MS;
  return new Promise((resolve, reject) => {
    const child = execFile(
      binary,
      args,
      { encoding: "utf8", timeout: timeoutMs, maxBuffer: MAX_OUTPUT_BYTES, env: options.env ?? process.env },
      (error, stdout, stderr) => {
        if (error === null) {
          resolve(stdout);
          return;
        }
        reject(toRunError(error, binary, args, stderr, timeoutMs));
      },
    );
    child.stdin?.end();
  });
}

export class TessClient {
  constructor(
    private readonly binary: string,
    private readonly options: RunOptions = {},
  ) {}

  async listAccounts(): Promise<ListRow[]> {
    return parseListRows(await runTess(this.binary, ["list", "--json"], this.options));
  }

  async copyCode(accountId: string, next: boolean): Promise<CodeRow> {
    const output = await runTess(this.binary, copyArguments(accountId, next), this.options);
    return selectCodeRow(parseCodeRows(output), accountId);
  }

  async getCode(accountId: string): Promise<CodeRow> {
    const output = await runTess(this.binary, codeArguments(accountId), this.options);
    return selectCodeRow(parseCodeRows(output), accountId);
  }

  async vaultStatus(): Promise<VaultStatus> {
    return parseVaultStatus(await runTess(this.binary, ["vault", "status", "--json"], this.options));
  }
}

export async function explainFailure(
  error: unknown,
  client: TessClient | undefined,
  env: NodeJS.ProcessEnv = process.env,
): Promise<Problem> {
  let status: VaultStatus | undefined;
  if (error instanceof TessExitError && client !== undefined) {
    status = await client.vaultStatus().catch(() => undefined);
  }
  return describeFailure(error, status, Boolean(env.TESSERA_PASSPHRASE));
}
