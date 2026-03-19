import { mkdir, readdir, rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const distDir = join(scriptDir, "../../internal/httpapi/dashboard/dist");

await mkdir(distDir, { recursive: true });

for (const entry of await readdir(distDir, { withFileTypes: true })) {
  if (entry.name === ".keep") {
    continue;
  }

  await rm(join(distDir, entry.name), {
    force: true,
    recursive: entry.isDirectory(),
  });
}
