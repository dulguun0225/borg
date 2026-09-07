// Puts ../../clientdist/browser/.gitkeep back after a build. The Angular build
// empties its output directory, and that one file is what lets the directory
// embed on a fresh clone whose client has not been built — the ground
// ../../clientdist/doc.go states.
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const keep = resolve(here, '../../clientdist/browser/.gitkeep');
mkdirSync(dirname(keep), { recursive: true });
if (!existsSync(keep)) {
  writeFileSync(keep, '');
}
