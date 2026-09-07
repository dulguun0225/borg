// Copies factoryVersion from ../../cmd/factory/main.go into
// ../src/environments/version.ts, which is the one constant a script writes
// into this source tree. The client carries the version on every call and the
// server refuses a call whose version is not its own, so the two must agree
// and neither may be edited alone.
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const mainGo = resolve(here, '../../cmd/factory/main.go');
const versionTs = resolve(here, '../src/environments/version.ts');

const found = /^const factoryVersion = "([^"]*)"$/m.exec(readFileSync(mainGo, 'utf8'));
if (found === null) {
  console.error(`no line reading 'const factoryVersion = "..."' in ${mainGo}`);
  process.exit(1);
}
const version = found[1];

const before = readFileSync(versionTs, 'utf8');
const after = before.replace(
  /^export const FACTORY_VERSION = '[^']*';$/m,
  `export const FACTORY_VERSION = '${version}';`,
);
if (after === before && !before.includes(`'${version}'`)) {
  console.error(`no line reading "export const FACTORY_VERSION = '...';" in ${versionTs}`);
  process.exit(1);
}
writeFileSync(versionTs, after);
console.log(`FACTORY_VERSION is ${version}`);
