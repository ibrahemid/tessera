import { describe, expect, it } from "vitest";
import { AccountMismatchError, selectCodeRow, toAccountItem } from "../src/lib/accounts";
import { ListRow, parseCodeRows, parseListRows } from "../src/lib/contract";
import { fixture } from "./helpers";

const rows = parseListRows(fixture("list.json"));
const byHandle = (handle: string): ListRow => {
  const row = rows.find((candidate) => candidate.handle === handle);
  if (row === undefined) throw new Error(`fixture has no ${handle}`);
  return row;
};

describe("toAccountItem", () => {
  it("titles an account by issuer and searches by every other field", () => {
    const item = toAccountItem(byHandle("gi"));
    expect(item.title).toBe("GitHub");
    expect(item.subtitle).toBe("alice@example.com");
    expect(item.keywords).toEqual(["gi", "alice@example.com", "Work", "prod", "dev"]);
    expect(item.folder).toBe("Work");
    expect(item.tags).toEqual(["prod", "dev"]);
    expect(item.pinned).toBe(true);
    expect(item.isCounterBased).toBe(false);
  });

  it("titles an account without an issuer by its account name", () => {
    const item = toAccountItem(byHandle("ex"));
    expect(item.title).toBe("bare");
    expect(item.subtitle).toBe("");
    expect(item.keywords).toEqual(["ex"]);
    expect(item.folder).toBe("");
    expect(item.tags).toEqual([]);
  });

  it("titles an account by handle when it has no names", () => {
    const item = toAccountItem({ ...byHandle("ex"), account: "" });
    expect(item.title).toBe("ex");
    expect(item.keywords).toEqual([]);
  });

  it("marks hotp accounts as counter based", () => {
    expect(toAccountItem(byHandle("ba")).isCounterBased).toBe(true);
    expect(toAccountItem(byHandle("st")).isCounterBased).toBe(false);
  });

  it("keeps the order tess printed", () => {
    expect(rows.map(toAccountItem).map((item) => item.id)).toEqual(rows.map((row) => row.id));
  });
});

describe("selectCodeRow", () => {
  const [totpRow] = parseCodeRows(fixture("code-totp.json"));

  it("returns the row for the requested account", () => {
    expect(selectCodeRow([totpRow], totpRow.id)).toBe(totpRow);
  });

  it("refuses a row for a different account", () => {
    expect(() => selectCodeRow([totpRow], byHandle("ac").id)).toThrow(AccountMismatchError);
  });

  it("refuses several rows, even when one is the requested account", () => {
    const ambiguous = parseCodeRows(fixture("code-ambiguous.json"));
    expect(() => selectCodeRow(ambiguous, byHandle("ex").id)).toThrow(AccountMismatchError);
  });
});
