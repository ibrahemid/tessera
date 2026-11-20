import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, it } from "node:test";
import { WORKFLOW_DIR } from "./helpers.mjs";

const PLIST_PATH = join(WORKFLOW_DIR, "info.plist");
const isDarwin = process.platform === "darwin";

function plutil(args) {
  return spawnSync("/usr/bin/plutil", args, { encoding: "utf8" });
}

function loadPlist() {
  const result = plutil(["-convert", "json", "-o", "-", PLIST_PATH]);
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(result.stdout);
}

function objectOfType(plist, type) {
  const matches = plist.objects.filter((object) => object.type === type);
  assert.equal(matches.length, 1, `expected one ${type}`);
  return matches[0];
}

describe("info.plist", { skip: !isDarwin && "plutil needs macOS" }, () => {
  it("passes plutil -lint", () => {
    const result = plutil(["-lint", PLIST_PATH]);
    assert.equal(result.status, 0, result.stdout + result.stderr);
  });

  it("connects only objects that exist", () => {
    const plist = loadPlist();
    const uids = new Set(plist.objects.map((object) => object.uid));
    assert.equal(uids.size, plist.objects.length);
    for (const [source, targets] of Object.entries(plist.connections)) {
      assert.ok(uids.has(source), `unknown source ${source}`);
      for (const target of targets) {
        assert.ok(uids.has(target.destinationuid), `unknown destination ${target.destinationuid}`);
        assert.equal(target.modifiers, 0);
      }
    }
    assert.deepEqual(Object.keys(plist.uidata).sort(), [...uids].sort());
  });

  it("runs tess.js from the script filter and the action", () => {
    const plist = loadPlist();
    const filter = objectOfType(plist, "alfred.workflow.input.scriptfilter");
    assert.equal(filter.config.keyword, "2fa");
    assert.equal(filter.config.alfredfiltersresults, false);
    assert.equal(filter.config.scriptargtype, 1);
    assert.equal(filter.config.script, '/usr/bin/osascript -l JavaScript ./tess.js list "${1}"');
    const action = objectOfType(plist, "alfred.workflow.action.script");
    assert.equal(action.config.script, "/usr/bin/osascript -l JavaScript ./tess.js action");
    assert.deepEqual(
      plist.connections[filter.uid].map((target) => target.destinationuid),
      [action.uid],
    );
  });

  it("pastes a paste result, copies a command result and notifies otherwise", () => {
    const plist = loadPlist();
    const conditional = objectOfType(plist, "alfred.workflow.utility.conditional");
    const notification = objectOfType(plist, "alfred.workflow.output.notification");
    const clipboards = plist.objects.filter((object) => object.type === "alfred.workflow.output.clipboard");
    const paste = clipboards.find((object) => object.config.autopaste === true);
    const copy = clipboards.find((object) => object.config.autopaste === false);
    assert.equal(clipboards.length, 2);
    assert.deepEqual(paste.config, {
      autopaste: true,
      clipboardtext: "{query}",
      ignoredynamicplaceholders: false,
      transient: true,
    });
    assert.deepEqual(copy.config, {
      autopaste: false,
      clipboardtext: "{query}",
      ignoredynamicplaceholders: false,
      transient: false,
    });
    const [pasteCondition, commandCondition] = conditional.config.conditions;
    assert.equal(conditional.config.conditions.length, 2);
    for (const condition of conditional.config.conditions) {
      assert.equal(condition.inputstring, "{var:tess_result}");
      assert.equal(condition.matchcasesensitive, true);
      assert.equal(condition.matchmode, 0);
    }
    assert.equal(pasteCondition.matchstring, "paste");
    assert.equal(commandCondition.matchstring, "command");
    assert.equal(notification.config.title, "{var:tess_notice}");
    assert.equal(notification.config.text, "{var:tess_detail}");
    const routes = plist.connections[conditional.uid];
    assert.deepEqual(
      routes.map((route) => [route.sourceoutputuid ?? "else", route.destinationuid]),
      [
        [pasteCondition.uid, paste.uid],
        [commandCondition.uid, copy.uid],
        [commandCondition.uid, notification.uid],
        ["else", notification.uid],
      ],
    );
  });

  it("offers the tess path and vault file as workflow configuration", () => {
    const plist = loadPlist();
    assert.deepEqual(
      plist.userconfigurationconfig.map((entry) => [entry.variable, entry.type, entry.config.required]),
      [
        ["tess_path", "textfield", false],
        ["vault_file", "textfield", false],
      ],
    );
  });
});
