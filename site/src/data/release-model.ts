export class ReleaseDataError extends Error {
  constructor(message: string) {
    super(`versions.json: ${message}`);
    this.name = "ReleaseDataError";
  }
}

export type CliFile = { label: string; file: string; sha256: string };
export type CliRelease = {
  version: string;
  date: string;
  platforms: string[];
  files: CliFile[];
  packages: string[];
  signature: string | null;
};
export type AppRelease = { version: string; date: string; minimumMacOS: string };
export type Versions = { cli: CliRelease; app: AppRelease };

export type PackageFormat = "deb" | "rpm" | "apk" | "pkg.tar.zst";
export type LinuxPackages = { formats: PackageFormat[]; arches: string[] };

const VERSION_PATTERN = /^(\d+)\.(\d+)\.(\d+)$/;
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const PACKAGE_FORMATS: PackageFormat[] = ["deb", "rpm", "apk", "pkg.tar.zst"];
const ARCHES = ["amd64", "arm64"];
const PLATFORM_BY_OS: Record<string, string> = { darwin: "macOS", linux: "Linux", windows: "Windows" };

export function parseVersion(version: string): [number, number, number] {
  const match = VERSION_PATTERN.exec(version);
  if (!match) throw new ReleaseDataError(`version ${JSON.stringify(version)} is not MAJOR.MINOR.PATCH`);
  return [Number(match[1]), Number(match[2]), Number(match[3])];
}

export function compareVersions(a: string, b: string): number {
  const left = parseVersion(a);
  const right = parseVersion(b);
  for (let i = 0; i < 3; i++) {
    if (left[i] !== right[i]) return left[i] < right[i] ? -1 : 1;
  }
  return 0;
}

export function isVersionAtLeast(version: string, minimum: string): boolean {
  return compareVersions(version, minimum) >= 0;
}

export function joinList(items: readonly string[], conjunction: "and" | "or" = "and"): string {
  if (items.length <= 1) return items.join("");
  return `${items.slice(0, -1).join(", ")} ${conjunction} ${items[items.length - 1]}`;
}

function archiveParts(cli: CliRelease, file: string): { os: string; arch: string; ext: string } {
  const prefix = `tess_${cli.version}_`;
  const match = /^(darwin|linux|windows)_(amd64|arm64)\.(tar\.gz|zip)$/.exec(file.startsWith(prefix) ? file.slice(prefix.length) : "");
  if (!match) throw new ReleaseDataError(`cli file ${JSON.stringify(file)} is not ${prefix}<darwin|linux|windows>_<amd64|arm64>.<tar.gz|zip>`);
  const [, os, arch, ext] = match;
  if ((os === "windows") !== (ext === "zip")) throw new ReleaseDataError(`cli file ${file}: Windows archives are .zip, the others .tar.gz`);
  return { os, arch, ext };
}

function packageParts(cli: CliRelease, file: string): { arch: string; format: PackageFormat } {
  const prefix = `tessera_${cli.version}_linux_`;
  const match = /^(amd64|arm64)\.(deb|rpm|apk|pkg\.tar\.zst)$/.exec(file.startsWith(prefix) ? file.slice(prefix.length) : "");
  if (!match) throw new ReleaseDataError(`cli package ${JSON.stringify(file)} is not ${prefix}<amd64|arm64>.<deb|rpm|apk|pkg.tar.zst>`);
  return { arch: match[1], format: match[2] as PackageFormat };
}

export function getArchives(cli: CliRelease, platform: string): CliFile[] {
  return cli.files.filter((f) => PLATFORM_BY_OS[archiveParts(cli, f.file).os] === platform);
}

export function getLinuxPackages(cli: CliRelease): LinuxPackages {
  const parts = cli.packages.map((file) => packageParts(cli, file));
  return {
    formats: PACKAGE_FORMATS.filter((format) => parts.some((p) => p.format === format)),
    arches: ARCHES.filter((arch) => parts.some((p) => p.arch === arch)),
  };
}

export function validateVersions(data: Versions): Versions {
  const { cli, app } = data;
  parseVersion(cli.version);
  parseVersion(app.version);
  for (const [name, date] of [["cli", cli.date], ["app", app.date]]) {
    if (!DATE_PATTERN.test(date)) throw new ReleaseDataError(`${name} date ${JSON.stringify(date)} is not YYYY-MM-DD`);
  }
  if (!/^\d+$/.test(app.minimumMacOS)) throw new ReleaseDataError(`app minimumMacOS ${JSON.stringify(app.minimumMacOS)} is not a major version`);

  const seen = new Set<string>();
  for (const f of cli.files) {
    if (seen.has(f.file)) throw new ReleaseDataError(`cli file ${f.file} is listed twice`);
    seen.add(f.file);
    if (!f.label.trim()) throw new ReleaseDataError(`cli file ${f.file} has no label`);
    if (!SHA256_PATTERN.test(f.sha256)) throw new ReleaseDataError(`cli file ${f.file} has no lowercase hex sha256`);
    archiveParts(cli, f.file);
  }
  const archivePlatforms = new Set(cli.files.map((f) => PLATFORM_BY_OS[archiveParts(cli, f.file).os]));
  for (const platform of cli.platforms) {
    if (!archivePlatforms.has(platform)) throw new ReleaseDataError(`cli platform ${platform} has no archive in files`);
  }
  for (const platform of archivePlatforms) {
    if (!cli.platforms.includes(platform)) throw new ReleaseDataError(`cli files carry ${platform} archives but platforms omits ${platform}`);
  }

  const packages = new Set<string>();
  for (const file of cli.packages) {
    if (packages.has(file)) throw new ReleaseDataError(`cli package ${file} is listed twice`);
    packages.add(file);
    packageParts(cli, file);
  }
  if (cli.packages.length > 0 && !cli.platforms.includes("Linux")) throw new ReleaseDataError("cli packages are listed but platforms omits Linux");
  if (cli.signature !== null && !/^[\w.-]+\.sigstore\.json$/.test(cli.signature)) {
    throw new ReleaseDataError(`cli signature ${JSON.stringify(cli.signature)} is not a .sigstore.json file name`);
  }
  return data;
}
