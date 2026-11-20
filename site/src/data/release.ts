import data from "./versions.json";
import { getArchives, getLinuxPackages, isVersionAtLeast, joinList, validateVersions, type Versions } from "./release-model";

export const versions = validateVersions(data as Versions);
export const CLI_PLATFORMS = joinList(versions.cli.platforms);
export const HAS_WINDOWS_BUILD = versions.cli.platforms.includes("Windows");
export const WINDOWS_ARCHIVES = getArchives(versions.cli, "Windows");
export const LINUX_PACKAGES = getLinuxPackages(versions.cli);
export const HAS_LINUX_PACKAGES = versions.cli.packages.length > 0;
export const CLI_SIGNATURE = versions.cli.signature;
export const SECRET_STORES = ["the login keychain on macOS", ...(HAS_WINDOWS_BUILD ? ["Windows Credential Manager"] : []), "the Secret Service on Linux, with the kernel keyring as a fallback"];
export const IS_VAULT_V2_RELEASED = isVersionAtLeast(versions.cli.version, "1.2.0");
