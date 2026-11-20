import goMod from "../../../go/go.mod?raw";

class ToolchainError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ToolchainError";
  }
}

function readGoVersion(mod: string): string {
  const match = mod.match(/^go (\d+\.\d+(?:\.\d+)?)\s*$/m);
  if (!match) throw new ToolchainError("go/go.mod has no go directive");
  return match[1];
}

export const GO_VERSION = readGoVersion(goMod);
