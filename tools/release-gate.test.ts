import { afterEach, expect, test } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(directories.splice(0).map((path) => rm(path, { recursive: true, force: true })));
});

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "tako-release-gate-"));
  directories.push(root);
  const manifest = join(root, "manifest.json");
  const evidence = join(root, "evidence", "target.json");
  const artifact = join(root, "packages", "tako_1.0_ubuntu_amd64.deb");
  await Bun.write(manifest, JSON.stringify({ targets: [{
    id: "ubuntu-amd64", package_name: "ubuntu_amd64", package_format: "deb",
  }] }));
  await Bun.write(artifact, "tested package");
  const record = {
    target: "ubuntu-amd64", status: "passed", exit_code: 0, commit: "tested-commit",
    package: { new_sha256: new Bun.CryptoHasher("sha256").update("tested package").digest("hex") },
  };
  await Bun.write(evidence, JSON.stringify(record));
  return { root, manifest, evidence, artifact, record };
}

async function gate(value: Awaited<ReturnType<typeof fixture>>) {
  const result = Bun.spawnSync({
    cmd: [process.execPath, join(import.meta.dir, "release-gate"),
      join(value.root, "evidence"), join(value.root, "packages"), "tested-commit"],
    env: { ...process.env, TAKO_RELEASE_MANIFEST: value.manifest },
    stdout: "pipe", stderr: "pipe",
  });
  return { code: result.exitCode, error: result.stderr.toString() };
}

test("accepts the exact package validated by the VM", async () => {
  expect((await gate(await fixture())).code).toBe(0);
});

test("rejects a substituted release package", async () => {
  const value = await fixture();
  await Bun.write(value.artifact, "untested package");
  const result = await gate(value);
  expect(result.code).toBe(1);
  expect(result.error).toContain("evidence digest does not match");
});

test("rejects evidence from another commit", async () => {
  const value = await fixture();
  await Bun.write(value.evidence, JSON.stringify({ ...value.record, commit: "old-commit" }));
  const result = await gate(value);
  expect(result.code).toBe(1);
  expect(result.error).toContain("expected tested-commit");
});

test("rejects missing VM evidence", async () => {
  const value = await fixture();
  await rm(value.evidence);
  const result = await gate(value);
  expect(result.code).toBe(1);
  expect(result.error).toContain("missing VM evidence");
});
