import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { describe, it } from "node:test";
import { scriptFilterProblems } from "./alfred-shape.mjs";
import { runWorkflow } from "./helpers.mjs";

const binary = process.env.TESS_TEST_BINARY;
const vault = process.env.TESSERA_VAULT;
const passphrase = process.env.TESSERA_PASSPHRASE;
const enabled = process.platform === "darwin" && binary && vault && passphrase;

const scratchEnv = Object.fromEntries(
  Object.entries({
    HOME: process.env.HOME,
    XDG_DATA_HOME: process.env.XDG_DATA_HOME,
    TESSERA_VAULT: vault,
    tess_path: binary,
  }).filter(([, value]) => value !== undefined),
);

function tessJson(args) {
  return JSON.parse(
    execFileSync(binary, args, { env: { ...scratchEnv, TESSERA_PASSPHRASE: passphrase }, encoding: "utf8" }),
  );
}

describe("tess.js against a built tess and a scratch vault", { skip: !enabled && "set TESS_TEST_BINARY" }, () => {
  it("lists the same accounts as tess list", () => {
    const response = runWorkflow(["list", ""], { ...scratchEnv, TESSERA_PASSPHRASE: passphrase });
    assert.deepEqual(scriptFilterProblems(response), []);
    assert.deepEqual(
      response.items.map((item) => item.uid),
      tessJson(["list", "--json"]).map((row) => row.id),
    );
  });

  it("reads the vault named by the vault file setting", () => {
    const { TESSERA_VAULT: _, ...withoutVault } = scratchEnv;
    const response = runWorkflow(["list", ""], { ...withoutVault, vault_file: vault, TESSERA_PASSPHRASE: passphrase });
    assert.deepEqual(
      response.items.map((item) => item.uid),
      tessJson(["list", "--json"]).map((row) => row.id),
    );
  });

  it("hands the code of each time-based account to Alfred for pasting", () => {
    const rows = tessJson(["list", "--json"]).filter((row) => row.type !== "hotp");
    assert.ok(rows.length > 0);
    for (const row of rows) {
      const before = tessJson(["code", "--json", "--", row.id]).code;
      const output = runWorkflow(["action"], {
        ...scratchEnv,
        TESSERA_PASSPHRASE: passphrase,
        tess_action: "paste",
        tess_id: row.id,
        tess_type: row.type,
        tess_title: row.issuer || row.account || row.handle,
      });
      const after = tessJson(["code", "--json", "--", row.id]).code;
      assert.equal(output.alfredworkflow.variables.tess_result, "paste", JSON.stringify(output));
      assert.ok([before, after].includes(output.alfredworkflow.arg));
    }
  });

  it("explains a locked vault when no passphrase is available", () => {
    const [item] = runWorkflow(["list", ""], scratchEnv).items;
    assert.equal(item.title, "Vault locked");
    assert.equal(item.arg, "tess vault remember");
  });
});
