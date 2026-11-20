import { describe, expect, it } from "vitest";
import { ContractError, parseCodeRows, parseListRows, parseVaultStatus } from "../src/lib/contract";
import { fixture } from "./helpers";

describe("parseListRows", () => {
  it("reads every row in display order", () => {
    const rows = parseListRows(fixture("list.json"));
    expect(rows.map((row) => row.handle)).toEqual(["gi", "ac", "ba", "ex", "st"]);
    expect(rows[0]).toEqual({
      id: "49abdd291b36981883aafdd2e47ced39",
      handle: "gi",
      issuer: "GitHub",
      account: "alice@example.com",
      type: "totp",
      folder: "Work",
      tags: ["prod", "dev"],
      pinned: true,
      hasNote: true,
    });
  });

  it("fills absent folder and tags on an account without them", () => {
    const bare = parseListRows(fixture("list.json"))[3];
    expect(bare).toEqual({
      id: "04acf1e4bab8cfa2a7dce2564cfb225b",
      handle: "ex",
      issuer: "",
      account: "bare",
      type: "totp",
      folder: "",
      tags: [],
      pinned: false,
      hasNote: false,
    });
  });

  it("ignores fields the contract does not list", () => {
    const row = parseListRows(fixture("list.json"))[2];
    expect(Object.keys(row).sort()).toEqual(
      ["account", "folder", "hasNote", "handle", "id", "issuer", "pinned", "tags", "type"].sort(),
    );
  });

  it("accepts an empty vault", () => {
    expect(parseListRows("[]\n")).toEqual([]);
  });

  it("rejects output that is not JSON", () => {
    expect(() => parseListRows("GitHub  219 515")).toThrow(ContractError);
  });

  it("rejects a row missing a required field", () => {
    const rows = JSON.parse(fixture("list.json")) as Record<string, unknown>[];
    delete rows[1].pinned;
    expect(() => parseListRows(JSON.stringify(rows))).toThrow(/list row 2: `pinned`/);
  });

  it("rejects an object where an array is expected", () => {
    expect(() => parseListRows(fixture("code-totp.json"))).toThrow(ContractError);
  });
});

describe("parseCodeRows", () => {
  it("reads a totp row with its next code", () => {
    expect(parseCodeRows(fixture("code-totp.json"))).toEqual([
      {
        id: "49abdd291b36981883aafdd2e47ced39",
        handle: "gi",
        issuer: "GitHub",
        account: "alice@example.com",
        type: "totp",
        code: "219515",
        nextCode: "368929",
        expiresIn: 18,
      },
    ]);
  });

  it("reads an hotp row that has no next code", () => {
    const [row] = parseCodeRows(fixture("code-hotp.json"));
    expect(row.code).toBe("969429");
    expect(row.nextCode).toBeUndefined();
    expect(row.expiresIn).toBeUndefined();
  });

  it("keeps steam codes as text", () => {
    const [row] = parseCodeRows(fixture("code-steam.json"));
    expect(row.code).toBe("3WWY8");
    expect(row.nextCode).toBe("JV52M");
  });

  it("reads every row when the query matched several accounts", () => {
    expect(parseCodeRows(fixture("code-ambiguous.json")).map((row) => row.handle)).toEqual(["ac", "ex"]);
  });

  it("rejects a code that is not a string", () => {
    const row = JSON.parse(fixture("code-totp.json")) as Record<string, unknown>;
    row.code = 219515;
    expect(() => parseCodeRows(JSON.stringify(row))).toThrow(/`code`/);
  });
});

describe("parseVaultStatus", () => {
  it("reads a vault whose passphrase is not remembered", () => {
    expect(parseVaultStatus(fixture("status-not-remembered.json"))).toEqual({
      path: "/Users/me/.local/share/tessera/vault.json",
      exists: true,
      unreadable: undefined,
      rememberedIn: [],
      lockedIn: [],
    });
  });

  it("reads a missing vault", () => {
    expect(parseVaultStatus(fixture("status-missing.json")).exists).toBe(false);
  });

  it("reads a locked secret store", () => {
    expect(parseVaultStatus(fixture("status-store-locked.json")).lockedIn).toEqual(["login keychain"]);
  });

  it("reads why a vault is unreadable", () => {
    expect(parseVaultStatus(fixture("status-unreadable.json")).unreadable).toMatch(/permission denied/);
  });
});
