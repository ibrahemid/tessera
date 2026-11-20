const TOP_LEVEL_KEYS = new Set([
  "items",
  "variables",
  "cache",
  "rerun",
  "iconresize",
  "preselect",
  "skipknowledge",
  "strictmods",
]);
const ITEM_KEYS = new Set([
  "uid",
  "title",
  "subtitle",
  "arg",
  "icon",
  "valid",
  "match",
  "autocomplete",
  "type",
  "mods",
  "action",
  "text",
  "quicklookurl",
  "variables",
]);
const MOD_KEYS = new Set(["valid", "arg", "subtitle", "icon", "variables"]);
const MODIFIERS = new Set(["cmd", "alt", "ctrl", "shift", "fn"]);
const ITEM_TYPES = new Set(["default", "file", "file:skipcheck"]);
const ICON_TYPES = new Set(["fileicon", "filetype"]);
const WORKFLOW_OUTPUT_KEYS = new Set(["arg", "config", "variables"]);

function isObject(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function checkKeys(value, allowed, where, problems) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) problems.push(`${where}: unknown key ${key}`);
  }
}

function checkString(value, where, problems) {
  if (typeof value !== "string") problems.push(`${where}: not a string`);
}

function checkArg(value, where, problems) {
  if (typeof value !== "string" && !(Array.isArray(value) && value.every((item) => typeof item === "string"))) {
    problems.push(`${where}: not a string or an array of strings`);
  }
}

function checkVariables(value, where, problems) {
  if (!isObject(value)) {
    problems.push(`${where}: not an object`);
    return;
  }
  for (const [key, entry] of Object.entries(value)) checkString(entry, `${where}.${key}`, problems);
}

function checkIcon(value, where, problems) {
  if (!isObject(value)) {
    problems.push(`${where}: not an object`);
    return;
  }
  checkString(value.path, `${where}.path`, problems);
  if (value.type !== undefined && !ICON_TYPES.has(value.type)) problems.push(`${where}.type: ${value.type}`);
}

function checkMods(value, where, problems) {
  if (!isObject(value)) {
    problems.push(`${where}: not an object`);
    return;
  }
  for (const [combo, mod] of Object.entries(value)) {
    const keys = combo.split("+");
    if (!keys.every((key) => MODIFIERS.has(key)) || new Set(keys).size !== keys.length) {
      problems.push(`${where}: invalid modifier ${combo}`);
    }
    const modWhere = `${where}.${combo}`;
    if (!isObject(mod)) {
      problems.push(`${modWhere}: not an object`);
      continue;
    }
    checkKeys(mod, MOD_KEYS, modWhere, problems);
    if (mod.valid !== undefined && typeof mod.valid !== "boolean") problems.push(`${modWhere}.valid: not a boolean`);
    if (mod.arg !== undefined) checkArg(mod.arg, `${modWhere}.arg`, problems);
    if (mod.subtitle !== undefined) checkString(mod.subtitle, `${modWhere}.subtitle`, problems);
    if (mod.icon !== undefined) checkIcon(mod.icon, `${modWhere}.icon`, problems);
    if (mod.variables !== undefined) checkVariables(mod.variables, `${modWhere}.variables`, problems);
  }
}

function checkItem(item, where, problems) {
  if (!isObject(item)) {
    problems.push(`${where}: not an object`);
    return;
  }
  checkKeys(item, ITEM_KEYS, where, problems);
  checkString(item.title, `${where}.title`, problems);
  for (const key of ["uid", "subtitle", "match", "autocomplete", "quicklookurl"]) {
    if (item[key] !== undefined) checkString(item[key], `${where}.${key}`, problems);
  }
  if (item.arg !== undefined) checkArg(item.arg, `${where}.arg`, problems);
  if (item.valid !== undefined && typeof item.valid !== "boolean") problems.push(`${where}.valid: not a boolean`);
  if (item.type !== undefined && !ITEM_TYPES.has(item.type)) problems.push(`${where}.type: ${item.type}`);
  if (item.icon !== undefined) checkIcon(item.icon, `${where}.icon`, problems);
  if (item.mods !== undefined) checkMods(item.mods, `${where}.mods`, problems);
  if (item.variables !== undefined) checkVariables(item.variables, `${where}.variables`, problems);
  if (item.text !== undefined) {
    if (!isObject(item.text)) {
      problems.push(`${where}.text: not an object`);
    } else {
      checkKeys(item.text, new Set(["copy", "largetype"]), `${where}.text`, problems);
      for (const [key, entry] of Object.entries(item.text)) checkString(entry, `${where}.text.${key}`, problems);
    }
  }
}

export function scriptFilterProblems(response) {
  const problems = [];
  if (!isObject(response)) return ["response: not an object"];
  checkKeys(response, TOP_LEVEL_KEYS, "response", problems);
  if (!Array.isArray(response.items)) {
    problems.push("response.items: not an array");
  } else {
    response.items.forEach((item, index) => checkItem(item, `items[${index}]`, problems));
  }
  if (response.variables !== undefined) checkVariables(response.variables, "response.variables", problems);
  if (response.skipknowledge !== undefined && typeof response.skipknowledge !== "boolean") {
    problems.push("response.skipknowledge: not a boolean");
  }
  return problems;
}

export function workflowOutputProblems(output) {
  const problems = [];
  if (!isObject(output)) return ["output: not an object"];
  checkKeys(output, new Set(["alfredworkflow"]), "output", problems);
  const body = output.alfredworkflow;
  if (!isObject(body)) return [...problems, "output.alfredworkflow: not an object"];
  checkKeys(body, WORKFLOW_OUTPUT_KEYS, "alfredworkflow", problems);
  if (body.arg !== undefined) checkArg(body.arg, "alfredworkflow.arg", problems);
  if (body.variables !== undefined) checkVariables(body.variables, "alfredworkflow.variables", problems);
  return problems;
}
