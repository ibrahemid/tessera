import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { scriptFilterProblems, workflowOutputProblems } from "./alfred-shape.mjs";
import { envFrom, fixture, loadWorkflow, plain, stderrFixture } from "./helpers.mjs";

const w = loadWorkflow();
const GI_ID = "49abdd291b36981883aafdd2e47ced39";
const BA_ID = "e79ef49cdf1b1bcd260e363fdaf921cd";
const EX_ID = "04acf1e4bab8cfa2a7dce2564cfb225b";
const ST_ID = "d8db694d88f80749163b421a5489f884";

const rows = () => w.parseListRows(fixture("list.json"));
const rowFor = (handle) => rows().find((row) => row.handle === handle);
const status = (name) => w.parseVaultStatus(fixture(`status-${name}.json`));
const exitError = (stderr) => new w.TessExitError(["list", "--json"], 1, stderr);

describe("parseListRows", () => {
  it("keeps the CLI order", () => {
    assert.deepEqual(plain(rows().map((row) => row.handle)), ["gi", "ac", "ba", "ex", "st"]);
  });

  it("fills defaults for an account with no folder, tags or note", () => {
    assert.deepEqual(plain(rowFor("ex")), {
      id: EX_ID,
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

  it("ignores unknown fields", () => {
    assert.deepEqual(Object.keys(rowFor("ba")).sort(), [
      "account",
      "folder",
      "handle",
      "hasNote",
      "id",
      "issuer",
      "pinned",
      "tags",
      "type",
    ]);
  });

  it("accepts an empty vault", () => {
    assert.deepEqual(plain(w.parseListRows("[]")), []);
  });

  it("rejects output that is not JSON", () => {
    assert.throws(() => w.parseListRows("Enter passphrase:"), w.ContractError);
  });

  it("names the row and field that break the contract", () => {
    const broken = JSON.parse(fixture("list.json"));
    delete broken[1].pinned;
    assert.throws(() => w.parseListRows(JSON.stringify(broken)), /list row 2: `pinned`/);
  });

  it("rejects an object where the list array belongs", () => {
    assert.throws(() => w.parseListRows(fixture("code-totp.json")), /not a JSON array/);
  });
});

describe("parseCodeRows", () => {
  it("reads a totp row and ignores unknown fields", () => {
    assert.deepEqual(plain(w.parseCodeRows(fixture("code-totp.json"))), [
      {
        id: GI_ID,
        handle: "gi",
        issuer: "GitHub",
        account: "alice@example.com",
        type: "totp",
        code: "219515",
        nextCode: "368929",
      },
    ]);
  });

  it("reads an hotp row without a next code", () => {
    const [row] = w.parseCodeRows(fixture("code-hotp.json"));
    assert.equal(row.code, "969429");
    assert.equal(row.nextCode, undefined);
  });

  it("reads several rows when the query matches several accounts", () => {
    assert.deepEqual(plain(w.parseCodeRows(fixture("code-ambiguous.json")).map((row) => row.handle)), ["ac", "ex"]);
  });

  it("rejects a row without a code", () => {
    assert.throws(() => w.parseCodeRows('{"id":"x","handle":"x","issuer":"","account":"","type":"totp"}'), /`code`/);
  });
});

describe("parseVaultStatus", () => {
  it("reads every status fixture", () => {
    assert.deepEqual(plain(status("remembered")).rememberedIn, ["login keychain"]);
    assert.deepEqual(plain(status("store-locked")).lockedIn, ["login keychain"]);
    assert.equal(status("missing").exists, false);
    assert.match(status("unreadable").unreadable, /permission denied/);
    assert.equal(status("not-remembered").unreadable, undefined);
  });
});

describe("accountItem", () => {
  it("builds a row with copy, next and paste variables", () => {
    assert.deepEqual(plain(w.accountItem(rowFor("gi"))), {
      uid: GI_ID,
      title: "GitHub",
      subtitle: "alice@example.com · Work · prod dev",
      arg: GI_ID,
      valid: true,
      variables: { tess_action: "copy", tess_id: GI_ID, tess_type: "totp", tess_title: "GitHub" },
      mods: {
        cmd: {
          valid: true,
          arg: GI_ID,
          subtitle: "Copy the next code",
          variables: { tess_action: "next", tess_id: GI_ID, tess_type: "totp", tess_title: "GitHub" },
        },
        alt: {
          valid: true,
          arg: GI_ID,
          subtitle: "Paste the code",
          variables: { tess_action: "paste", tess_id: GI_ID, tess_type: "totp", tess_title: "GitHub" },
        },
      },
    });
  });

  it("titles an account without an issuer by its account name", () => {
    const item = w.accountItem(rowFor("ex"));
    assert.equal(item.title, "bare");
    assert.equal(item.subtitle, "");
  });

  it("describes the counter advance on an hotp account", () => {
    assert.equal(w.accountItem(rowFor("ba")).mods.cmd.subtitle, "Advance the counter and copy the code");
  });
});

describe("buildListResponse", () => {
  const titles = (response) => plain(response.items.map((item) => item.title));

  it("lists every account in CLI order for an empty query", () => {
    const response = w.buildListResponse(rows(), "");
    assert.deepEqual(titles(response), ["GitHub", "Acme Corp", "Bank", "bare", "Steam"]);
    assert.deepEqual(scriptFilterProblems(plain(response)), []);
    assert.equal(response.skipknowledge, true);
  });

  it("matches every query word against handle, issuer, account, folder and tags", () => {
    assert.deepEqual(titles(w.buildListResponse(rows(), "work")), ["GitHub", "Steam"]);
    assert.deepEqual(titles(w.buildListResponse(rows(), "PROD git")), ["GitHub"]);
    assert.deepEqual(titles(w.buildListResponse(rows(), "acme.io")), ["Acme Corp"]);
  });

  it("says when nothing matches", () => {
    const response = plain(w.buildListResponse(rows(), "zzz"));
    assert.deepEqual(response.items, [{ title: "No matching accounts", subtitle: "", valid: false }]);
    assert.deepEqual(scriptFilterProblems(response), []);
  });

  it("says when the vault has no accounts", () => {
    const response = plain(w.buildListResponse([], ""));
    assert.deepEqual(response.items, [{ title: "No accounts", subtitle: "Add one with tess add.", valid: false }]);
  });
});

describe("buildProblemResponse", () => {
  it("makes a problem with a command actionable", () => {
    const response = plain(
      w.buildProblemResponse(w.describeFailure(exitError(stderrFixture("locked")), status("not-remembered"), false)),
    );
    assert.deepEqual(scriptFilterProblems(response), []);
    assert.deepEqual(response.items[0], {
      title: "Vault locked",
      subtitle: "Run tess vault remember in a terminal so tess can unlock the vault without a prompt, then try again.",
      valid: true,
      arg: "tess vault remember",
      variables: { tess_action: "command", tess_command: "tess vault remember" },
      text: {
        largetype:
          "Run tess vault remember in a terminal so tess can unlock the vault without a prompt, then try again.",
        copy: "tess vault remember",
      },
    });
  });

  it("leaves a problem without a command inert", () => {
    const response = plain(w.buildProblemResponse(w.describeFailure(new w.TessTimeoutError(["list"], 15))));
    assert.deepEqual(scriptFilterProblems(response), []);
    assert.equal(response.items[0].valid, false);
    assert.equal(response.items[0].variables, undefined);
  });
});

describe("describeFailure", () => {
  it("explains a missing tess", () => {
    assert.match(w.describeFailure(new w.TessMissingError(undefined)).message, /workflow configuration/);
    assert.equal(
      w.describeFailure(new w.TessMissingError("/nope/tess")).message,
      "Nothing runnable at /nope/tess. Fix the path in the workflow configuration.",
    );
  });

  it("explains a locked vault with no remembered passphrase", () => {
    const failure = w.describeFailure(exitError(stderrFixture("locked")), status("not-remembered"), false);
    assert.equal(failure.kind, "not-remembered");
    assert.equal(failure.command, "tess vault remember");
  });

  it("falls back to stderr when the passphrase comes from the environment", () => {
    const failure = w.describeFailure(exitError(stderrFixture("locked")), status("not-remembered"), true);
    assert.equal(failure.kind, "failed");
  });

  it("explains a locked keychain", () => {
    assert.equal(
      w.describeFailure(exitError(stderrFixture("locked")), status("store-locked"), false).message,
      "Unlock the login keychain, then try again.",
    );
  });

  it("explains a missing vault", () => {
    const failure = w.describeFailure(exitError(stderrFixture("no_vault")), status("missing"), false);
    assert.equal(failure.kind, "no-vault");
    assert.equal(failure.command, "tess vault init");
    assert.equal(
      failure.message,
      "There is no vault at /Users/me/.local/share/tessera/vault.json. Create one with tess vault init in a terminal, or set the vault file in the workflow configuration.",
    );
  });

  it("explains a vault file setting that is not a full path", () => {
    assert.deepEqual(plain(w.describeFailure(new w.VaultFileSettingError("vault.json"))), {
      kind: "vault-file",
      title: "Vault file not usable",
      message: "vault.json is not a full path. Fix the vault file in the workflow configuration.",
    });
  });

  it("passes on why the vault is unreadable", () => {
    assert.equal(
      w.describeFailure(exitError(""), status("unreadable"), false).message,
      "open /Users/me/.local/share/tessera/vault.json: permission denied",
    );
  });

  it("shows the first stderr line when the vault is remembered", () => {
    assert.deepEqual(plain(w.describeFailure(exitError(stderrFixture("no_match")), status("remembered"), false)), {
      kind: "failed",
      title: "tess failed",
      message: 'no account matches "gi"',
    });
  });

  it("names the exit status when stderr is empty and no status is known", () => {
    assert.equal(w.describeFailure(exitError("\n"), undefined, false).message, "tess list exited with status 1.");
  });

  it("explains a changed account and unexpected output", () => {
    assert.equal(w.describeFailure(new w.AccountMismatchError("changed")).kind, "account-changed");
    assert.equal(w.describeFailure(new w.ContractError("bad")).message, "bad.");
  });

  it("reports any other error as a failed action", () => {
    assert.deepEqual(plain(w.describeFailure(new w.RequestError("no account"))), {
      kind: "failed",
      title: "Action failed",
      message: "no account",
    });
  });
});

describe("resolveTess", () => {
  const executables = (paths) => (path) => paths.includes(path);

  it("expands ~ in the configured path", () => {
    assert.equal(w.resolveTess("~/bin/tess", "/Users/me", executables(["/Users/me/bin/tess"])), "/Users/me/bin/tess");
  });

  it("rejects a relative or non-executable configured path", () => {
    assert.throws(() => w.resolveTess("bin/tess", "/Users/me", () => true), w.TessMissingError);
    assert.throws(() => w.resolveTess("/usr/bin/tess", "/Users/me", () => false), /\/usr\/bin\/tess is not/);
  });

  it("searches the install paths in order when nothing is configured", () => {
    const found = w.resolveTess(" ", "/Users/me", executables(["/Users/me/go/bin/tess", "/usr/local/bin/tess"]));
    assert.equal(found, "/usr/local/bin/tess");
    assert.equal(
      w.resolveTess(undefined, "/Users/me", executables(["/Users/me/go/bin/tess"])),
      "/Users/me/go/bin/tess",
    );
  });

  it("reports a missing tess with no configured path", () => {
    assert.throws(
      () => w.resolveTess("", "/Users/me", () => false),
      (error) => error instanceof w.TessMissingError && error.configuredPath === undefined,
    );
  });
});

describe("resolveVaultFile", () => {
  it("leaves the vault to tess when nothing is configured", () => {
    assert.equal(w.resolveVaultFile(undefined, "/Users/me"), undefined);
    assert.equal(w.resolveVaultFile("  ", "/Users/me"), undefined);
  });

  it("expands ~ and keeps a full path", () => {
    assert.equal(w.resolveVaultFile(" ~/vault.json ", "/Users/me"), "/Users/me/vault.json");
    assert.equal(w.resolveVaultFile("/Volumes/keys/vault.json", "/Users/me"), "/Volumes/keys/vault.json");
  });

  it("refuses a relative path", () => {
    assert.throws(
      () => w.resolveVaultFile("vault.json", "/Users/me"),
      (error) => error instanceof w.VaultFileSettingError && error.configuredPath === "vault.json",
    );
  });
});

describe("readActionRequest and codeArguments", () => {
  const request = (values) => w.readActionRequest(envFrom(values));

  it("reads an account action", () => {
    assert.deepEqual(plain(request({ tess_action: "copy", tess_id: GI_ID, tess_type: "totp", tess_title: "GitHub" })), {
      action: "copy",
      id: GI_ID,
      type: "totp",
      title: "GitHub",
    });
  });

  it("reads a command action", () => {
    assert.deepEqual(plain(request({ tess_action: "command", tess_command: "tess vault remember" })), {
      action: "command",
      command: "tess vault remember",
    });
  });

  it("rejects an unknown action and a missing account", () => {
    assert.throws(() => request({ tess_action: "delete", tess_id: GI_ID }), w.RequestError);
    assert.throws(() => request({ tess_action: "copy" }), w.RequestError);
    assert.throws(() => request({ tess_action: "command" }), w.RequestError);
  });

  it("has tess copy and clear the code, and reads it for a paste, by id after --", () => {
    const args = (values) => plain(w.codeArguments(request({ tess_id: GI_ID, ...values })));
    assert.deepEqual(args({ tess_action: "copy", tess_type: "totp" }), [
      "code",
      "-c",
      "--clear",
      "30",
      "--json",
      "--",
      GI_ID,
    ]);
    for (const type of ["totp", "hotp"]) {
      assert.deepEqual(args({ tess_action: "next", tess_type: type }), [
        "code",
        "-c",
        "--clear",
        "30",
        "--json",
        "--next",
        "--",
        GI_ID,
      ]);
    }
    assert.deepEqual(args({ tess_action: "paste", tess_type: "hotp" }), ["code", "--json", "--", GI_ID]);
  });
});

describe("selectCodeRow", () => {
  const select = (file, id) => w.selectCodeRow(w.parseCodeRows(fixture(file)), id);

  it("returns the row for the requested account", () => {
    assert.equal(select("code-totp.json", GI_ID).code, "219515");
    assert.equal(select("code-steam.json", ST_ID).code, "3WWY8");
    assert.equal(select("code-hotp.json", BA_ID).code, "969429");
  });

  it("refuses a row for another account and several rows", () => {
    assert.throws(() => select("code-totp.json", EX_ID), w.AccountMismatchError);
    assert.throws(() => select("code-ambiguous.json", EX_ID), w.AccountMismatchError);
  });
});

describe("action outputs", () => {
  it("match the alfredworkflow JSON shape", () => {
    const failure = w.describeFailure(new w.TessTimeoutError(["code"], 15));
    const outputs = [
      w.copiedOutput("Copied GitHub"),
      w.commandOutput("tess vault init"),
      w.pasteOutput("219515"),
      w.errorOutput(failure),
    ];
    for (const output of outputs) {
      assert.deepEqual(workflowOutputProblems(plain(output)), []);
    }
    assert.deepEqual(plain(w.commandOutput("tess vault init")), {
      alfredworkflow: {
        arg: "tess vault init",
        variables: { tess_result: "command", tess_notice: "Copied tess vault init", tess_detail: "" },
      },
    });
    assert.deepEqual(plain(w.pasteOutput("219515")), {
      alfredworkflow: { arg: "219515", variables: { tess_result: "paste", tess_notice: "", tess_detail: "" } },
    });
    assert.deepEqual(plain(w.errorOutput(failure)).alfredworkflow.variables, {
      tess_result: "error",
      tess_notice: "tess did not respond",
      tess_detail: "tess code did not finish within 15 seconds.",
    });
  });
});

describe("alfred-shape", () => {
  it("flags keys and types outside the documented format", () => {
    assert.deepEqual(scriptFilterProblems({ items: [{ title: "x", mods: { super: {} }, variables: { a: 1 } }] }), [
      "items[0].mods: invalid modifier super",
      "items[0].variables.a: not a string",
    ]);
    assert.deepEqual(scriptFilterProblems({ items: [{ subtitle: "x", extra: true }], foo: 1 }), [
      "response: unknown key foo",
      "items[0]: unknown key extra",
      "items[0].title: not a string",
    ]);
    assert.deepEqual(workflowOutputProblems({ alfredworkflow: { arg: 3, result: "x" } }), [
      "alfredworkflow: unknown key result",
      "alfredworkflow.arg: not a string or an array of strings",
    ]);
  });
});
