import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { validateReleaseSource } from "./release-metadata.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const release = await validateReleaseSource(root);

console.log(
  `Validated Hobot Code ${release.version}, Pi ${release.pi.PI_VERSION}, `
    + `fd ${release.tools.FD_VERSION}, and ripgrep ${release.tools.RIPGREP_VERSION}`,
);
