import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ReleaseDataError,
  compareVersions,
  getArchives,
  getLinuxPackages,
  isVersionAtLeast,
  joinList,
  parseVersion,
  validateVersions,
} from "../src/data/release-model.ts";

const SHA = "a".repeat(64);
const archive = (version, os, arch) => ({
  label: `${os} ${arch}`,
  file: `tess_${version}_${os}_${arch}.${os === "windows" ? "zip" : "tar.gz"}`,
  sha256: SHA,
});
const app = { version: "1.1.0", date: "2026-09-05", minimumMacOS: "14" };
const unixCli = (version = "1.1.0") => ({
  version,
  date: "2026-09-05",
  platforms: ["macOS", "Linux"],
  files: [archive(version, "darwin", "arm64"), archive(version, "darwin", "amd64"), archive(version, "linux", "arm64"), archive(version, "linux", "amd64")],
  packages: [],
  signature: null,
});
const fullCli = () => {
  const cli = unixCli("1.2.0");
  cli.platforms = ["macOS", "Linux", "Windows"];
  cli.files.push(archive("1.2.0", "windows", "amd64"), archive("1.2.0", "windows", "arm64"));
  cli.packages = ["amd64", "arm64"].flatMap((arch) => ["deb", "rpm", "apk", "pkg.tar.zst"].map((ext) => `tessera_1.2.0_linux_${arch}.${ext}`));
  cli.signature = "checksums.txt.sigstore.json";
  return cli;
};

test("parseVersion reads MAJOR.MINOR.PATCH and rejects anything else", () => {
  assert.deepEqual(parseVersion("1.12.3"), [1, 12, 3]);
  for (const bad of ["1.2", "v1.2.0", "1.2.0-rc1", ""]) assert.throws(() => parseVersion(bad), ReleaseDataError);
});

test("compareVersions orders numerically, not as text", () => {
  assert.equal(compareVersions("1.10.0", "1.9.9"), 1);
  assert.equal(compareVersions("1.2.0", "1.2.0"), 0);
  assert.equal(compareVersions("1.1.9", "1.2.0"), -1);
  assert.equal(compareVersions("2.0.0", "1.99.99"), 1);
});

test("isVersionAtLeast gates on the minimum inclusively", () => {
  assert.equal(isVersionAtLeast("1.1.0", "1.2.0"), false);
  assert.equal(isVersionAtLeast("1.2.0", "1.2.0"), true);
  assert.equal(isVersionAtLeast("1.3.1", "1.2.0"), true);
});

test("joinList joins with commas and a final conjunction", () => {
  assert.equal(joinList([]), "");
  assert.equal(joinList(["macOS"]), "macOS");
  assert.equal(joinList(["macOS", "Linux"]), "macOS and Linux");
  assert.equal(joinList(["macOS", "Linux", "Windows"]), "macOS, Linux and Windows");
  assert.equal(joinList(["macOS", "Linux", "Windows"], "or"), "macOS, Linux or Windows");
});

test("validateVersions accepts a release without Windows, packages or signature", () => {
  const data = { cli: unixCli(), app };
  assert.equal(validateVersions(data), data);
  assert.deepEqual(getArchives(data.cli, "Windows"), []);
  assert.deepEqual(getLinuxPackages(data.cli), { formats: [], arches: [] });
});

test("validateVersions accepts the full release config output", () => {
  const cli = fullCli();
  validateVersions({ cli, app });
  assert.deepEqual(getArchives(cli, "Windows").map((f) => f.file), ["tess_1.2.0_windows_amd64.zip", "tess_1.2.0_windows_arm64.zip"]);
  assert.deepEqual(getLinuxPackages(cli), { formats: ["deb", "rpm", "apk", "pkg.tar.zst"], arches: ["amd64", "arm64"] });
});

test("validateVersions rejects files and packages that do not match the version", () => {
  const stale = fullCli();
  stale.files[0] = archive("1.1.0", "darwin", "arm64");
  assert.throws(() => validateVersions({ cli: stale, app }), /tess_1\.2\.0_/);
  const stalePackage = fullCli();
  stalePackage.packages[0] = "tessera_1.1.0_linux_amd64.deb";
  assert.throws(() => validateVersions({ cli: stalePackage, app }), /tessera_1\.2\.0_linux_/);
});

test("validateVersions keeps platforms and archives in step", () => {
  const missingWindows = fullCli();
  missingWindows.platforms = ["macOS", "Linux"];
  assert.throws(() => validateVersions({ cli: missingWindows, app }), /platforms omits Windows/);
  const noArchive = unixCli();
  noArchive.platforms.push("Windows");
  assert.throws(() => validateVersions({ cli: noArchive, app }), /Windows has no archive/);
  const tarOnWindows = fullCli();
  tarOnWindows.files[4] = { ...tarOnWindows.files[4], file: "tess_1.2.0_windows_amd64.tar.gz" };
  assert.throws(() => validateVersions({ cli: tarOnWindows, app }), /Windows archives are \.zip/);
});

test("validateVersions rejects bad checksums, dates, duplicates and signature names", () => {
  const badSha = unixCli();
  badSha.files[0] = { ...badSha.files[0], sha256: "ABC" };
  assert.throws(() => validateVersions({ cli: badSha, app }), /sha256/);
  assert.throws(() => validateVersions({ cli: { ...unixCli(), date: "5 Sep 2026" }, app }), /cli date/);
  const duplicate = unixCli();
  duplicate.files.push(duplicate.files[0]);
  assert.throws(() => validateVersions({ cli: duplicate, app }), /listed twice/);
  assert.throws(() => validateVersions({ cli: { ...unixCli(), signature: "checksums.txt.sig" }, app }), /sigstore/);
  assert.throws(() => validateVersions({ cli: { ...unixCli(), packages: ["tessera_1.1.0_linux_amd64.deb"], platforms: ["macOS"], files: unixCli().files.slice(0, 2) }, app }), /omits Linux/);
});
