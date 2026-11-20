import { describe, expect, it } from "vitest";
import { TessClient, explainFailure, tessEnvironment } from "../src/lib/tess";

const binary = process.env.TESS_TEST_BINARY;
const vault = process.env.TESSERA_VAULT;
const enabled = binary !== undefined && vault !== undefined && process.env.TESSERA_PASSPHRASE !== undefined;

describe.skipIf(!enabled)("against a built tess and a scratch vault", () => {
  const client = new TessClient(binary ?? "");

  it("lists accounts and reads the code of each time-based one", async () => {
    const accounts = await client.listAccounts();
    expect(accounts.length).toBeGreaterThan(0);
    for (const account of accounts.filter((candidate) => candidate.type !== "hotp")) {
      const code = await client.getCode(account.id);
      expect(code.id).toBe(account.id);
      expect(code.code).toMatch(/^[0-9A-Z]{5,10}$/);
      expect(code.nextCode).toBeDefined();
    }
  });

  it("reads the vault named by the vault file setting", async () => {
    const env = { ...process.env };
    delete env.TESSERA_VAULT;
    const configured = new TessClient(binary ?? "", { env: tessEnvironment(vault, undefined, env) });
    expect((await configured.listAccounts()).length).toBeGreaterThan(0);
  });

  it("explains a locked vault when no passphrase is available", async () => {
    const env = { ...process.env };
    delete env.TESSERA_PASSPHRASE;
    const locked = new TessClient(binary ?? "", { env });
    const failure = await locked.listAccounts().catch((error: unknown) => error);
    const problem = await explainFailure(failure, locked, env);
    expect(problem.kind).toBe("not-remembered");
  });
});
