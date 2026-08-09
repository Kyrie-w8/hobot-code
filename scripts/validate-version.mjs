import { readFile } from "node:fs/promises";

const root = new URL("../", import.meta.url);
const version = (await readFile(new URL("VERSION", root), "utf8")).trim();
const packageJson = JSON.parse(await readFile(new URL("pi-runtime/package.json", root), "utf8"));
const lock = await readFile(new URL("pi-runtime/pi.lock", root), "utf8");

if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error(`VERSION is not semantic: ${version}`);
}
if (packageJson.version !== version) {
  throw new Error(`VERSION ${version} does not match pi-runtime/package.json ${packageJson.version}`);
}
for (const key of ["PI_VERSION", "PI_COMMIT", "PI_LINUX_ARM64_SHA256", "PI_LINUX_ARM64_URL"]) {
  if (!new RegExp(`^${key}=\\S+$`, "m").test(lock)) throw new Error(`pi.lock is missing ${key}`);
}
if (!/^PI_COMMIT=[0-9a-f]{40}$/m.test(lock)) throw new Error("PI_COMMIT must be a 40-character Git commit");

console.log(`Validated Hobot Code ${version} and Pi runtime provenance`);
