"use strict";

const TESS_TIMEOUT_SECONDS = 15;
const CLEAR_AFTER_SECONDS = 30;
const ACCOUNT_ACTIONS = ["copy", "next", "paste"];

class ContractError extends Error {
  constructor(message) {
    super(message);
    this.name = "ContractError";
  }
}

class AccountMismatchError extends Error {
  constructor(message) {
    super(message);
    this.name = "AccountMismatchError";
  }
}

class TessMissingError extends Error {
  constructor(configuredPath) {
    super(configuredPath === undefined ? "tess was not found" : `${configuredPath} is not an executable file`);
    this.name = "TessMissingError";
    this.configuredPath = configuredPath;
  }
}

class VaultFileSettingError extends Error {
  constructor(configuredPath) {
    super(`${configuredPath} is not a full path`);
    this.name = "VaultFileSettingError";
    this.configuredPath = configuredPath;
  }
}

class TessTimeoutError extends Error {
  constructor(args, timeoutSeconds) {
    super(`tess ${args[0]} did not finish within ${timeoutSeconds} seconds`);
    this.name = "TessTimeoutError";
  }
}

class TessExitError extends Error {
  constructor(args, exitCode, stderr) {
    super(`tess ${args[0]} exited with status ${exitCode}`);
    this.name = "TessExitError";
    this.exitCode = exitCode;
    this.stderr = stderr;
  }
}

class RequestError extends Error {
  constructor(message) {
    super(message);
    this.name = "RequestError";
  }
}

function parseJson(text, what) {
  try {
    return JSON.parse(text);
  } catch {
    throw new ContractError(`tess printed a ${what} that is not valid JSON`);
  }
}

function asObject(value, where) {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new ContractError(`${where} is not a JSON object`);
  }
  return value;
}

function readString(obj, key, where) {
  const value = obj[key];
  if (typeof value !== "string") {
    throw new ContractError(`${where}: \`${key}\` is missing or not a string`);
  }
  return value;
}

function readOptionalString(obj, key, where) {
  return obj[key] === undefined ? undefined : readString(obj, key, where);
}

function readBoolean(obj, key, where) {
  const value = obj[key];
  if (typeof value !== "boolean") {
    throw new ContractError(`${where}: \`${key}\` is missing or not a boolean`);
  }
  return value;
}

function readStringArray(obj, key, where, required) {
  const value = obj[key];
  if (value === undefined && !required) {
    return [];
  }
  if (!Array.isArray(value) || !value.every((item) => typeof item === "string")) {
    throw new ContractError(`${where}: \`${key}\` is missing or not an array of strings`);
  }
  return value;
}

function parseListRows(text) {
  const value = parseJson(text, "account list");
  if (!Array.isArray(value)) {
    throw new ContractError("the account list is not a JSON array");
  }
  return value.map((item, index) => {
    const where = `list row ${index + 1}`;
    const obj = asObject(item, where);
    return {
      id: readString(obj, "id", where),
      handle: readString(obj, "handle", where),
      issuer: readString(obj, "issuer", where),
      account: readString(obj, "account", where),
      type: readString(obj, "type", where),
      folder: readOptionalString(obj, "folder", where) ?? "",
      tags: readStringArray(obj, "tags", where, false),
      pinned: readBoolean(obj, "pinned", where),
      hasNote: readBoolean(obj, "has_note", where),
    };
  });
}

function parseCodeRows(text) {
  const value = parseJson(text, "code");
  return (Array.isArray(value) ? value : [value]).map((item, index) => {
    const where = `code row ${index + 1}`;
    const obj = asObject(item, where);
    return {
      id: readString(obj, "id", where),
      handle: readString(obj, "handle", where),
      issuer: readString(obj, "issuer", where),
      account: readString(obj, "account", where),
      type: readString(obj, "type", where),
      code: readString(obj, "code", where),
      nextCode: readOptionalString(obj, "next_code", where),
    };
  });
}

function parseVaultStatus(text) {
  const where = "vault status";
  const obj = asObject(parseJson(text, where), where);
  return {
    path: readString(obj, "path", where),
    exists: readBoolean(obj, "exists", where),
    unreadable: readOptionalString(obj, "unreadable", where),
    rememberedIn: readStringArray(obj, "remembered_in", where, true),
    lockedIn: readStringArray(obj, "locked_in", where, true),
  };
}

function accountTitle(row) {
  return row.issuer || row.account || row.handle;
}

function accountSubtitle(row) {
  return [row.issuer ? row.account : "", row.folder, row.tags.join(" ")].filter((part) => part !== "").join(" · ");
}

function matchesQuery(row, query) {
  const haystack = [row.handle, row.issuer, row.account, row.folder, ...row.tags].join("\n").toLowerCase();
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter((word) => word !== "")
    .every((word) => haystack.includes(word));
}

function accountVariables(row, action) {
  return {
    tess_action: action,
    tess_id: row.id,
    tess_type: row.type,
    tess_title: accountTitle(row),
  };
}

function accountItem(row) {
  const nextSubtitle = row.type === "hotp" ? "Advance the counter and copy the code" : "Copy the next code";
  return {
    uid: row.id,
    title: accountTitle(row),
    subtitle: accountSubtitle(row),
    arg: row.id,
    valid: true,
    variables: accountVariables(row, "copy"),
    mods: {
      cmd: { valid: true, arg: row.id, subtitle: nextSubtitle, variables: accountVariables(row, "next") },
      alt: { valid: true, arg: row.id, subtitle: "Paste the code", variables: accountVariables(row, "paste") },
    },
  };
}

function noticeItem(title, subtitle) {
  return { title, subtitle, valid: false };
}

function buildListResponse(rows, query) {
  if (rows.length === 0) {
    return { skipknowledge: true, items: [noticeItem("No accounts", "Add one with tess add.")] };
  }
  const items = rows.filter((row) => matchesQuery(row, query)).map(accountItem);
  return { skipknowledge: true, items: items.length > 0 ? items : [noticeItem("No matching accounts", "")] };
}

function buildProblemResponse(problem) {
  const item = { title: problem.title, subtitle: problem.message, valid: false, text: { largetype: problem.message } };
  if (problem.command !== undefined) {
    item.valid = true;
    item.arg = problem.command;
    item.variables = { tess_action: "command", tess_command: problem.command };
    item.text.copy = problem.command;
  }
  return { skipknowledge: true, items: [item] };
}

function problem(kind, title, message, command) {
  return { kind, title, message, command };
}

function firstErrorLine(stderr) {
  const line = stderr
    .split("\n")
    .map((text) => text.trim())
    .find((text) => text !== "");
  return line === undefined ? "" : line.replace(/^error:\s*/, "");
}

function describeExit(error, status, passphraseFromEnv) {
  if (status !== undefined) {
    if (!status.exists) {
      return problem(
        "no-vault",
        "No vault",
        `There is no vault at ${status.path}. Create one with tess vault init in a terminal, or set the vault file in the workflow configuration.`,
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

function describeFailure(error, status, passphraseFromEnv) {
  if (error instanceof TessMissingError) {
    return error.configuredPath === undefined
      ? problem("missing", "tess not found", "Install tess, or set its path in the workflow configuration.")
      : problem(
          "missing",
          "tess not found",
          `Nothing runnable at ${error.configuredPath}. Fix the path in the workflow configuration.`,
        );
  }
  if (error instanceof VaultFileSettingError) {
    return problem(
      "vault-file",
      "Vault file not usable",
      `${error.configuredPath} is not a full path. Fix the vault file in the workflow configuration.`,
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
  return problem("failed", "Action failed", error instanceof Error ? error.message : String(error));
}

function expandHome(path, home) {
  return path === "~" || path.startsWith("~/") ? home + path.slice(1) : path;
}

function resolveVaultFile(configuredPath, home) {
  const configured = (configuredPath ?? "").trim();
  if (configured === "") {
    return undefined;
  }
  const expanded = expandHome(configured, home);
  if (!expanded.startsWith("/")) {
    throw new VaultFileSettingError(configured);
  }
  return expanded;
}

function resolveTess(configuredPath, home, isExecutableFile) {
  const configured = (configuredPath ?? "").trim();
  if (configured !== "") {
    const expanded = expandHome(configured, home);
    if (expanded.startsWith("/") && isExecutableFile(expanded)) {
      return expanded;
    }
    throw new TessMissingError(expanded);
  }
  const found = [
    "/opt/homebrew/bin/tess",
    "/usr/local/bin/tess",
    `${home}/.local/bin/tess`,
    `${home}/go/bin/tess`,
  ].find(isExecutableFile);
  if (found === undefined) {
    throw new TessMissingError(undefined);
  }
  return found;
}

function readActionRequest(env) {
  const action = env("tess_action") ?? "";
  if (action === "command") {
    const command = env("tess_command") ?? "";
    if (command === "") {
      throw new RequestError("The workflow sent a command action without a command.");
    }
    return { action, command };
  }
  if (!ACCOUNT_ACTIONS.includes(action)) {
    throw new RequestError(`The workflow sent an unknown action: ${action || "none"}.`);
  }
  const id = env("tess_id") ?? "";
  if (id === "") {
    throw new RequestError("The workflow sent an action without an account.");
  }
  return { action, id, type: env("tess_type") ?? "", title: env("tess_title") || id };
}

function codeArguments(request) {
  if (request.action === "paste") {
    return ["code", "--json", "--", request.id];
  }
  return [
    "code",
    "-c",
    "--clear",
    String(CLEAR_AFTER_SECONDS),
    "--json",
    ...(request.action === "next" ? ["--next"] : []),
    "--",
    request.id,
  ];
}

function selectCodeRow(rows, accountId) {
  if (rows.length === 1 && rows[0].id === accountId) {
    return rows[0];
  }
  throw new AccountMismatchError("tess answered for a different account. Reopen the list and try again.");
}

function workflowOutput(arg, variables) {
  return { alfredworkflow: { arg, variables } };
}

function copiedOutput(notice) {
  return workflowOutput("", { tess_result: "copied", tess_notice: notice, tess_detail: "" });
}

function commandOutput(command) {
  return workflowOutput(command, { tess_result: "command", tess_notice: `Copied ${command}`, tess_detail: "" });
}

function pasteOutput(code) {
  return workflowOutput(code, { tess_result: "paste", tess_notice: "", tess_detail: "" });
}

function errorOutput(failure) {
  return workflowOutput("", { tess_result: "error", tess_notice: failure.title, tess_detail: failure.message });
}

function readEnvironment() {
  const values = ObjC.deepUnwrap($.NSProcessInfo.processInfo.environment);
  return (name) => {
    const value = values[name];
    return typeof value === "string" ? value : undefined;
  };
}

function isExecutableFile(path) {
  const manager = $.NSFileManager.defaultManager;
  const isDirectory = Ref();
  return (
    manager.fileExistsAtPathIsDirectory(path, isDirectory) && !isDirectory[0] && manager.isExecutableFileAtPath(path)
  );
}

function locateTess(env) {
  const home = env("HOME") ?? "";
  return {
    binary: resolveTess(env("tess_path"), home, isExecutableFile),
    vault: resolveVaultFile(env("vault_file"), home),
  };
}

const pipeData = {};
let isPipeReaderRegistered = false;

function registerPipeReader() {
  if (isPipeReaderRegistered) {
    return;
  }
  isPipeReaderRegistered = true;
  ObjC.registerSubclass({
    name: "TessPipeReader",
    methods: {
      "readCompleted:": {
        types: ["void", ["id"]],
        implementation: (note) => {
          pipeData[ObjC.unwrap(note.object.description)] = note.userInfo.objectForKey(
            $.NSFileHandleNotificationDataItem,
          );
        },
      },
    },
  });
}

function decodeData(data) {
  if (data === undefined || data.isNil()) {
    return "";
  }
  const text = $.NSString.alloc.initWithDataEncoding(data, $.NSUTF8StringEncoding);
  return text.isNil() ? "" : text.js;
}

function runTess(tess, args, env) {
  registerPipeReader();
  const timeoutSeconds = Number(env("TESS_ALFRED_TEST_TIMEOUT")) || TESS_TIMEOUT_SECONDS;
  const task = $.NSTask.alloc.init;
  task.executableURL = $.NSURL.fileURLWithPath(tess.binary);
  task.arguments = args;
  if (tess.vault !== undefined) {
    const environment = $.NSMutableDictionary.dictionaryWithDictionary($.NSProcessInfo.processInfo.environment);
    environment.setObjectForKey(tess.vault, "TESSERA_VAULT");
    task.environment = environment;
  }
  task.standardInput = $.NSFileHandle.fileHandleWithNullDevice;
  const outPipe = $.NSPipe.pipe;
  const errPipe = $.NSPipe.pipe;
  task.standardOutput = outPipe;
  task.standardError = errPipe;
  const outHandle = outPipe.fileHandleForReading;
  const errHandle = errPipe.fileHandleForReading;
  const outKey = ObjC.unwrap(outHandle.description);
  const errKey = ObjC.unwrap(errHandle.description);
  const reader = $.TessPipeReader.alloc.init;
  const center = $.NSNotificationCenter.defaultCenter;
  center.addObserverSelectorNameObject(
    reader,
    "readCompleted:",
    $.NSFileHandleReadToEndOfFileCompletionNotification,
    outHandle,
  );
  center.addObserverSelectorNameObject(
    reader,
    "readCompleted:",
    $.NSFileHandleReadToEndOfFileCompletionNotification,
    errHandle,
  );
  try {
    if (!task.launchAndReturnError($())) {
      throw new TessMissingError(tess.binary);
    }
    outHandle.readToEndOfFileInBackgroundAndNotify;
    errHandle.readToEndOfFileInBackgroundAndNotify;
    const deadline = Date.now() + timeoutSeconds * 1000;
    while (task.running || !(outKey in pipeData) || !(errKey in pipeData)) {
      if (Date.now() > deadline) {
        if (task.running) {
          task.terminate;
        }
        throw new TessTimeoutError(args, timeoutSeconds);
      }
      $.NSRunLoop.currentRunLoop.runModeBeforeDate($.NSDefaultRunLoopMode, $.NSDate.dateWithTimeIntervalSinceNow(0.05));
    }
  } finally {
    center.removeObserver(reader);
  }
  const stdout = decodeData(pipeData[outKey]);
  const stderr = decodeData(pipeData[errKey]);
  delete pipeData[outKey];
  delete pipeData[errKey];
  if (task.terminationStatus !== 0) {
    throw new TessExitError(args, task.terminationStatus, stderr);
  }
  return stdout;
}

function explainFailure(error, tess, env) {
  let status;
  if (error instanceof TessExitError && tess !== undefined) {
    try {
      status = parseVaultStatus(runTess(tess, ["vault", "status", "--json"], env));
    } catch {
      status = undefined;
    }
  }
  return describeFailure(error, status, (env("TESSERA_PASSPHRASE") ?? "") !== "");
}

function listAccounts(env, query) {
  let tess;
  try {
    tess = locateTess(env);
    return buildListResponse(parseListRows(runTess(tess, ["list", "--json"], env)), query);
  } catch (error) {
    return buildProblemResponse(explainFailure(error, tess, env));
  }
}

function performAction(env) {
  let tess;
  try {
    const request = readActionRequest(env);
    if (request.action === "command") {
      return commandOutput(request.command);
    }
    tess = locateTess(env);
    const row = selectCodeRow(parseCodeRows(runTess(tess, codeArguments(request), env)), request.id);
    if (request.action === "paste") {
      return pasteOutput(row.code);
    }
    return copiedOutput(
      request.action === "next" ? `Copied the next code for ${request.title}` : `Copied ${request.title}`,
    );
  } catch (error) {
    return errorOutput(explainFailure(error, tess, env));
  }
}

function run(argv) {
  ObjC.import("Foundation");
  const env = readEnvironment();
  if (argv[0] === "list") {
    return JSON.stringify(listAccounts(env, argv[1] ?? ""));
  }
  if (argv[0] === "action") {
    return JSON.stringify(performAction(env));
  }
  throw new Error(`Unknown mode: ${argv[0]}`);
}
