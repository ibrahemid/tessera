import {
  Action,
  ActionPanel,
  Clipboard,
  Icon,
  List,
  Toast,
  getPreferenceValues,
  openExtensionPreferences,
  showHUD,
  showToast,
} from "@raycast/api";
import { usePromise } from "@raycast/utils";
import { AccountItem, toAccountItem } from "./lib/accounts";
import { Problem } from "./lib/problems";
import { TessClient, explainFailure, locateTess, tessEnvironment } from "./lib/tess";

type CodeAction = "copy" | "copy-next" | "paste";

type LoadResult = { accounts: AccountItem[]; problem: undefined } | { accounts: undefined; problem: Problem };

async function createClient(): Promise<TessClient> {
  const { tessPath, vaultFile } = getPreferenceValues<Preferences.SearchAccounts>();
  const env = tessEnvironment(vaultFile);
  return new TessClient(await locateTess(tessPath), { env });
}

async function loadAccounts(): Promise<LoadResult> {
  let client: TessClient | undefined;
  try {
    client = await createClient();
    const rows = await client.listAccounts();
    return { accounts: rows.map(toAccountItem), problem: undefined };
  } catch (error) {
    return { accounts: undefined, problem: await explainFailure(error, client) };
  }
}

async function deliverCode(item: AccountItem, action: CodeAction): Promise<void> {
  const toast = await showToast({ style: Toast.Style.Animated, title: "Getting code" });
  let client: TessClient | undefined;
  try {
    client = await createClient();
    if (action === "paste") {
      const row = await client.getCode(item.id);
      await toast.hide();
      await Clipboard.paste(row.code);
      return;
    }
    await client.copyCode(item.id, action === "copy-next");
    await toast.hide();
    await showHUD(action === "copy-next" ? `Copied the next code for ${item.title}` : `Copied ${item.title}`);
  } catch (error) {
    const problem = await explainFailure(error, client);
    toast.style = Toast.Style.Failure;
    toast.title = problem.title;
    toast.message = problem.message;
    const command = problem.command;
    if (command !== undefined) {
      toast.primaryAction = { title: "Copy Command", onAction: () => Clipboard.copy(command) };
    }
  }
}

function accessories(item: AccountItem): List.Item.Accessory[] {
  const result: List.Item.Accessory[] = item.tags.map((tag) => ({ tag }));
  if (item.folder !== "") {
    result.push({ icon: Icon.Folder, text: item.folder, tooltip: "Folder" });
  }
  if (item.pinned) {
    result.push({ icon: Icon.Pin, tooltip: "Pinned" });
  }
  return result;
}

function AccountActions({ item }: { item: AccountItem }) {
  return (
    <ActionPanel>
      <Action title="Copy Code" icon={Icon.Clipboard} onAction={() => deliverCode(item, "copy")} />
      <Action title="Paste Code" icon={Icon.Text} onAction={() => deliverCode(item, "paste")} />
      <Action
        title={item.isCounterBased ? "Advance Counter and Copy Code" : "Copy Next Code"}
        icon={Icon.Forward}
        shortcut={{ modifiers: ["cmd", "shift"], key: "n" }}
        onAction={() => deliverCode(item, "copy-next")}
      />
    </ActionPanel>
  );
}

function ProblemView({ problem, onRetry }: { problem: Problem; onRetry: () => void }) {
  return (
    <List.EmptyView
      icon={Icon.ExclamationMark}
      title={problem.title}
      description={problem.message}
      actions={
        <ActionPanel>
          {problem.command !== undefined && <Action.CopyToClipboard title="Copy Command" content={problem.command} />}
          {(problem.kind === "missing" || problem.kind === "vault-file" || problem.kind === "no-vault") && (
            <Action title="Open Extension Preferences" icon={Icon.Gear} onAction={openExtensionPreferences} />
          )}
          <Action title="Try Again" icon={Icon.ArrowClockwise} onAction={onRetry} />
        </ActionPanel>
      }
    />
  );
}

export default function Command() {
  const { data, isLoading, revalidate } = usePromise(loadAccounts);

  return (
    <List isLoading={isLoading} searchBarPlaceholder="Search accounts">
      {data?.problem !== undefined && <ProblemView problem={data.problem} onRetry={revalidate} />}
      {data?.accounts?.length === 0 && (
        <List.EmptyView icon={Icon.Key} title="No accounts" description="Add one with tess add." />
      )}
      {data?.accounts?.map((item) => (
        <List.Item
          key={item.id}
          title={item.title}
          subtitle={item.subtitle}
          keywords={item.keywords}
          accessories={accessories(item)}
          actions={<AccountActions item={item} />}
        />
      ))}
    </List>
  );
}
