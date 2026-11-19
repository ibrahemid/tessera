import { CodeRow, ListRow } from "./contract";

export interface AccountItem {
  id: string;
  title: string;
  subtitle: string;
  keywords: string[];
  folder: string;
  tags: string[];
  pinned: boolean;
  isCounterBased: boolean;
}

export class AccountMismatchError extends Error {
  constructor(
    message: string,
    readonly accountId: string,
  ) {
    super(message);
    this.name = "AccountMismatchError";
  }
}

export function toAccountItem(row: ListRow): AccountItem {
  const title = row.issuer || row.account || row.handle;
  const subtitle = row.issuer ? row.account : "";
  const keywords = [row.handle, row.issuer, row.account, row.folder, ...row.tags].filter(
    (word, index, all) => word !== "" && word !== title && all.indexOf(word) === index,
  );
  return {
    id: row.id,
    title,
    subtitle,
    keywords,
    folder: row.folder,
    tags: row.tags,
    pinned: row.pinned,
    isCounterBased: row.type === "hotp",
  };
}

export function selectCodeRow(rows: CodeRow[], accountId: string): CodeRow {
  if (rows.length === 1 && rows[0].id === accountId) {
    return rows[0];
  }
  throw new AccountMismatchError("tess answered for a different account. Reopen the list and try again.", accountId);
}
