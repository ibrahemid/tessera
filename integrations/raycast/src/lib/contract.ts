export interface ListRow {
  id: string;
  handle: string;
  issuer: string;
  account: string;
  type: string;
  folder: string;
  tags: string[];
  pinned: boolean;
  hasNote: boolean;
}

export interface CodeRow {
  id: string;
  handle: string;
  issuer: string;
  account: string;
  type: string;
  code: string;
  nextCode: string | undefined;
  expiresIn: number | undefined;
}

export interface VaultStatus {
  path: string;
  exists: boolean;
  unreadable: string | undefined;
  rememberedIn: string[];
  lockedIn: string[];
}

export class ContractError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ContractError";
  }
}

type JsonObject = Record<string, unknown>;

function parseJson(text: string, what: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    throw new ContractError(`tess printed a ${what} that is not valid JSON`);
  }
}

function asObject(value: unknown, where: string): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new ContractError(`${where} is not a JSON object`);
  }
  return value as JsonObject;
}

function readString(obj: JsonObject, key: string, where: string): string {
  const value = obj[key];
  if (typeof value !== "string") {
    throw new ContractError(`${where}: \`${key}\` is missing or not a string`);
  }
  return value;
}

function readOptionalString(obj: JsonObject, key: string, where: string): string | undefined {
  if (obj[key] === undefined) {
    return undefined;
  }
  return readString(obj, key, where);
}

function readBoolean(obj: JsonObject, key: string, where: string): boolean {
  const value = obj[key];
  if (typeof value !== "boolean") {
    throw new ContractError(`${where}: \`${key}\` is missing or not a boolean`);
  }
  return value;
}

function readOptionalInteger(obj: JsonObject, key: string, where: string): number | undefined {
  const value = obj[key];
  if (value === undefined) {
    return undefined;
  }
  if (typeof value !== "number" || !Number.isInteger(value)) {
    throw new ContractError(`${where}: \`${key}\` is not an integer`);
  }
  return value;
}

function readStringArray(obj: JsonObject, key: string, where: string, required: boolean): string[] {
  const value = obj[key];
  if (value === undefined && !required) {
    return [];
  }
  if (!Array.isArray(value) || !value.every((item) => typeof item === "string")) {
    throw new ContractError(`${where}: \`${key}\` is missing or not an array of strings`);
  }
  return value;
}

function readListRow(value: unknown, index: number): ListRow {
  const where = `list row ${index + 1}`;
  const obj = asObject(value, where);
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
}

function readCodeRow(value: unknown, index: number): CodeRow {
  const where = `code row ${index + 1}`;
  const obj = asObject(value, where);
  return {
    id: readString(obj, "id", where),
    handle: readString(obj, "handle", where),
    issuer: readString(obj, "issuer", where),
    account: readString(obj, "account", where),
    type: readString(obj, "type", where),
    code: readString(obj, "code", where),
    nextCode: readOptionalString(obj, "next_code", where),
    expiresIn: readOptionalInteger(obj, "expires_in", where),
  };
}

export function parseListRows(text: string): ListRow[] {
  const value = parseJson(text, "account list");
  if (!Array.isArray(value)) {
    throw new ContractError("the account list is not a JSON array");
  }
  return value.map(readListRow);
}

export function parseCodeRows(text: string): CodeRow[] {
  const value = parseJson(text, "code");
  const rows = Array.isArray(value) ? value : [value];
  return rows.map(readCodeRow);
}

export function parseVaultStatus(text: string): VaultStatus {
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
