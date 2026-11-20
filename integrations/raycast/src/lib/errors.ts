export class TessMissingError extends Error {
  constructor(readonly configuredPath: string | undefined) {
    super(
      configuredPath === undefined
        ? "tess is not installed in a known location"
        : `no runnable tess at ${configuredPath}`,
    );
    this.name = "TessMissingError";
  }
}

export class TessTimeoutError extends Error {
  constructor(
    readonly args: string[],
    readonly timeoutMs: number,
  ) {
    super(`tess ${args[0]} did not finish within ${timeoutMs / 1000} seconds`);
    this.name = "TessTimeoutError";
  }
}

export class TessExitError extends Error {
  constructor(
    readonly args: string[],
    readonly exitCode: number | null,
    readonly stderr: string,
  ) {
    super(`tess ${args[0]} exited with status ${exitCode ?? "unknown"}`);
    this.name = "TessExitError";
  }
}

export class VaultFileSettingError extends Error {
  constructor(readonly configuredPath: string) {
    super(`${configuredPath} is not a full path`);
    this.name = "VaultFileSettingError";
  }
}
