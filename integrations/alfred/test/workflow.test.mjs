import assert from "node:assert/strict";
import { afterEach, beforeEach, describe, it } from "node:test";
import { scriptFilterProblems, workflowOutputProblems } from "./alfred-shape.mjs";
import { createFakeTess, createScratchHome, fixture, runWorkflow, stderrFixture } from "./helpers.mjs";

const GI_ID = "49abdd291b36981883aafdd2e47ced39";
const BA_ID = "e79ef49cdf1b1bcd260e363fdaf921cd";
const EX_ID = "04acf1e4bab8cfa2a7dce2564cfb225b";
const isDarwin = process.platform === "darwin";

describe("tess.js under osascript", { skip: !isDarwin && "osascript needs macOS" }, () => {
  let scratch;
  let fake;

  beforeEach(() => {
    scratch = createScratchHome();
  });

  afterEach(() => {
    fake?.remove();
    fake = undefined;
    scratch.remove();
  });

  const env = (extra) => ({
    ...scratch.env,
    tess_path: fake.binary,
    ...extra,
  });

  const list = (query, extra) => {
    const response = runWorkflow(["list", query], env(extra));
    assert.deepEqual(scriptFilterProblems(response), []);
    return response;
  };

  const action = (variables) => {
    const output = runWorkflow(["action"], env(variables));
    assert.deepEqual(workflowOutputProblems(output), []);
    return output.alfredworkflow;
  };

  const account = (action, id, type, title) => ({
    tess_action: action,
    tess_id: id,
    tess_type: type,
    tess_title: title,
  });

  describe("list", () => {
    it("lists and filters accounts", () => {
      fake = createFakeTess({ list: { stdout: fixture("list.json") } });
      const response = list("work");
      assert.deepEqual(
        response.items.map((item) => [item.uid, item.title]),
        [
          [GI_ID, "GitHub"],
          ["d8db694d88f80749163b421a5489f884", "Steam"],
        ],
      );
      assert.deepEqual(fake.calls(), [["list", "--json"]]);
    });

    it("explains a locked vault and offers the command", () => {
      fake = createFakeTess({
        list: { stderr: stderrFixture("locked"), exitCode: 1 },
        status: { stdout: fixture("status-not-remembered.json") },
      });
      const [item] = list("").items;
      assert.equal(item.title, "Vault locked");
      assert.equal(item.arg, "tess vault remember");
      assert.deepEqual(fake.calls(), [
        ["list", "--json"],
        ["vault", "status", "--json"],
      ]);
    });

    it("explains a missing tess", () => {
      fake = createFakeTess({});
      const [item] = list("", { tess_path: `${scratch.env.HOME}/missing/tess` }).items;
      assert.equal(item.title, "tess not found");
      assert.match(item.subtitle, /missing\/tess/);
    });

    it("stops a tess that hangs", () => {
      fake = createFakeTess({ list: { sleepSeconds: 20 } });
      const started = Date.now();
      const [item] = list("", { TESS_ALFRED_TEST_TIMEOUT: "1" }).items;
      assert.equal(item.title, "tess did not respond");
      assert.ok(Date.now() - started < 10_000);
    });

    it("explains output that breaks the contract", () => {
      fake = createFakeTess({ list: { stdout: "Enter passphrase:" } });
      assert.equal(list("").items[0].title, "Unexpected output from tess");
    });

    it("passes the vault file to tess and to its status check", () => {
      fake = createFakeTess({
        list: { stderr: stderrFixture("no_vault"), exitCode: 1 },
        status: { stdout: fixture("status-missing.json") },
      });
      const [item] = list("", { vault_file: "~/vaults/work.json" }).items;
      assert.equal(item.title, "No vault");
      const expected = `${scratch.env.HOME}/vaults/work.json`;
      assert.deepEqual(fake.vaults(), [expected, expected]);
    });

    it("leaves TESSERA_VAULT alone without a vault file", () => {
      fake = createFakeTess({ list: { stdout: fixture("list.json") } });
      list("");
      assert.deepEqual(fake.vaults(), [scratch.env.TESSERA_VAULT]);
    });

    it("explains a relative vault file without running tess", () => {
      fake = createFakeTess({ list: { stdout: fixture("list.json") } });
      const [item] = list("", { vault_file: "vault.json" }).items;
      assert.equal(item.title, "Vault file not usable");
      assert.deepEqual(fake.calls(), []);
    });
  });

  describe("action", () => {
    it("has tess copy and clear the current code", () => {
      fake = createFakeTess({ code: { stdout: fixture("code-totp.json"), stderr: "copied\n" } });
      const output = action(account("copy", GI_ID, "totp", "GitHub"));
      assert.deepEqual(output, {
        arg: "",
        variables: { tess_result: "copied", tess_notice: "Copied GitHub", tess_detail: "" },
      });
      assert.deepEqual(fake.calls(), [["code", "-c", "--clear", "30", "--json", "--", GI_ID]]);
    });

    it("has tess copy the next code", () => {
      fake = createFakeTess({ code: { stdout: fixture("code-hotp.json") } });
      const output = action(account("next", BA_ID, "hotp", "Bank"));
      assert.equal(output.variables.tess_notice, "Copied the next code for Bank");
      assert.deepEqual(fake.calls(), [["code", "-c", "--clear", "30", "--json", "--next", "--", BA_ID]]);
    });

    it("hands the code to Alfred for pasting", () => {
      fake = createFakeTess({ code: { stdout: fixture("code-totp.json") } });
      const output = action(account("paste", GI_ID, "totp", "GitHub"));
      assert.equal(output.arg, "219515");
      assert.equal(output.variables.tess_result, "paste");
      assert.deepEqual(fake.calls(), [["code", "--json", "--", GI_ID]]);
    });

    it("passes the vault file to tess", () => {
      fake = createFakeTess({ code: { stdout: fixture("code-totp.json") } });
      action({ ...account("copy", GI_ID, "totp", "GitHub"), vault_file: "/Volumes/keys/vault.json" });
      assert.deepEqual(fake.vaults(), ["/Volumes/keys/vault.json"]);
    });

    it("refuses a code that belongs to another account", () => {
      fake = createFakeTess({ code: { stdout: fixture("code-totp.json") } });
      const output = action(account("paste", EX_ID, "totp", "bare"));
      assert.equal(output.variables.tess_result, "error");
      assert.equal(output.variables.tess_notice, "Could not get the code");
      assert.equal(output.arg, "");
    });

    it("reports a tess error with its first stderr line", () => {
      fake = createFakeTess({
        code: { stderr: stderrFixture("no_match"), exitCode: 1 },
        status: { stdout: fixture("status-remembered.json") },
      });
      const output = action(account("copy", GI_ID, "totp", "GitHub"));
      assert.deepEqual(output.variables, {
        tess_result: "error",
        tess_notice: "tess failed",
        tess_detail: 'no account matches "gi"',
      });
    });

    it("hands a suggested command to Alfred's clipboard without running tess", () => {
      fake = createFakeTess({});
      const output = action({ tess_action: "command", tess_command: "tess vault remember" });
      assert.deepEqual(output, {
        arg: "tess vault remember",
        variables: { tess_result: "command", tess_notice: "Copied tess vault remember", tess_detail: "" },
      });
      assert.deepEqual(fake.calls(), []);
    });
  });
});
