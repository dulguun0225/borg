// The one process the browser run drives: a factory of this commit, built and
// started here, serving the client's own build output on 8090. Playwright's
// webServer runs this and kills it when the run ends.
//
// The store is one fixed schema, factory_e2e, dropped and created here before
// the process starts and left in the database after the run: the run writes
// records through the screens, and a run that started on what the last one
// left would decide from rows it did not write. It is not the way a database
// test in ../../ makes its store — those name a schema per run and drop it at
// cleanup — so two runs on one DATABASE_URL are what this cannot survive: the
// second drops the schema the first is serving from.
//
// The directory holding the binary, the secrets file and the targets is one
// fixed path for the same reason. It is removed and made again at the start
// and removed at every exit this process can act on, and Playwright ends the
// web server with a signal no process handles — so what the start does is
// what makes the last run's directory go.
//
// The model is a name no provider answers. Nothing dispatches an agent unless
// an intent is supplied, and no test supplies one, so the run reaches no
// provider; the name is what makes a run that did fail at its own answer
// rather than spend a credential.
//
// It answers one route of its own, on 8091, with the owner's per-person key.
// That key is what an acting call is exempted by, -human names the owner by
// name rather than by key, and the key the name resolves to is written at the
// install and read nowhere a screen can reach — so a browser run that wrote
// anything would have to be told it. Playwright waits on that route rather
// than on the factory's own /healthz, which is what leaves no window in which
// a test could ask for the key before this has it.
import { existsSync, rmSync, writeFileSync, mkdirSync } from 'node:fs';
import { spawn, spawnSync } from 'node:child_process';
import { createServer } from 'node:http';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as after } from 'node:timers/promises';
import { tmpdir } from 'node:os';

const here = dirname(fileURLToPath(import.meta.url));
const factory = resolve(here, '../..');

// The binary embeds whatever ../../clientdist/browser holds at the moment it is
// built, so a run against an unbuilt client would drive the last build or none.
const index = resolve(factory, 'clientdist/browser/index.html');
if (!existsSync(index)) {
  console.error(`no ${index}: run npm run build first, the binary embedding whatever that directory holds`);
  process.exit(1);
}

// The database the run writes into, read the way postgres.URL() reads it.
const DEFAULT_URL = 'postgres://factory:factory@localhost:5433/factory';
const url = process.env['DATABASE_URL'] ?? DEFAULT_URL;
const SCHEMA = 'factory_e2e';

// A drop that cascades names every table it reaches, one notice per table, and
// the run's log is the factory's own output — so the server is asked for
// warnings and above and the notices are left unsaid.
const quiet = { ...process.env, PGOPTIONS: '-c client_min_messages=warning' };

const dropped = spawnSync(
  'psql',
  [url, '-v', 'ON_ERROR_STOP=1',
    '-c', `drop schema if exists ${SCHEMA} cascade`,
    '-c', `create schema ${SCHEMA}`],
  { env: quiet, stdio: ['ignore', 'ignore', 'inherit'] },
);
if (dropped.error !== undefined && dropped.error.code === 'ENOENT') {
  console.error('psql is not on the path, and the browser run drops and creates its own schema with it');
  process.exit(1);
}
if (dropped.status !== 0) {
  console.error(`psql could not drop and create schema ${SCHEMA} on ${url}: the database has to be reachable and the schema is this run's own`);
  process.exit(1);
}

// The child reads DATABASE_URL for the store it opens, and search_path is how
// every unqualified name in the DDL and in the writers' statements resolves to
// the schema above — the way inSchema in ../../cmd/factory/fixtures_test.go
// hands a subcommand a schema of its own.
const child = new URL(url);
child.searchParams.set('search_path', SCHEMA);

// The name -human gives the owner, and the name the mapping written at the
// install carries. The key it resolves to is what the browser declares.
const OWNER = 'e2e-owner';

const dir = join(tmpdir(), 'factory-e2e');
rmSync(dir, { recursive: true, force: true });
mkdirSync(dir);
const secrets = join(dir, 'secrets');
const targets = join(dir, 'targets');
const service = join(dir, 'e2e-service');
writeFileSync(secrets, 'model.openrouter=e2e-no-model\ndeploy.local=e2e-credential\n');
mkdirSync(targets);

// The route this answers with the owner's key, opened once there is a key to
// answer with.
let answering = null;
// True where this process is ending on something it refused rather than on the
// factory's own exit: the factory is ended by a signal from here, and a signal
// is no exit code, so without this the refusal would exit 0.
let failing = false;

// The directory goes on every path out this process can act on — a refusal
// above, the factory's own exit, and a signal it is given. A signal it cannot
// handle is what the start's removal is for.
const clean = () => {
  const open = answering;
  answering = null;
  open?.close();
  rmSync(dir, { recursive: true, force: true });
};
process.on('exit', clean);

const binary = join(dir, 'factory');
const built = spawnSync('go', ['build', '-o', binary, './cmd/factory'], {
  cwd: factory,
  stdio: 'inherit',
});
if (built.status !== 0) {
  console.error('the factory did not build, and the browser run drives the binary of this commit');
  process.exit(1);
}

// A binary and not `go run`: the process Playwright kills has to be the one
// serving the port, and `go run` holds a child of its own that outlives it.
const serving = spawn(
  binary,
  ['serve',
    '-port', '8090',
    '-secrets', secrets,
    '-model', 'e2e/no-model',
    '-service', `e2e-service=${service}`,
    '-area', 'e2e',
    '-project', 'e2e',
    // The owner every authoring write this process makes is made as, and the
    // one key an acting call exempts from the read-only-row refusal: an
    // install whose declaration is empty is what a fresh one is, so every
    // other key holds nothing and acts nowhere.
    '-human', OWNER,
    '-targets', targets],
  { cwd: factory, stdio: 'inherit', env: { ...process.env, DATABASE_URL: child.toString() } },
);

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    serving.kill(signal);
  });
}
serving.on('exit', (code, signal) => {
  if (failing) {
    process.exit(1);
  }
  process.exit(code ?? (signal === null ? 1 : 0));
});
serving.on('error', (cause) => {
  console.error(`the factory did not start: ${cause.message}`);
  process.exit(1);
});

// The factory's own /healthz, which answers once the install has run and the
// server is listening. The budget is two minutes, under Playwright's own for
// this command, so a factory that never serves is said so here rather than
// reported as a web server that never came up. The mapping the owner's key is
// read from is written before /healthz answers, so one read of the store after
// this is enough.
async function serves() {
  for (let attempt = 0; attempt < 600; attempt++) {
    try {
      if ((await fetch('http://127.0.0.1:8090/healthz')).ok) {
        return true;
      }
    } catch {
      // Not listening yet, which is what the next attempt is for.
    }
    await after(200);
  }
  return false;
}

// Ends the factory and this process with it, on something this refused.
const refuse = (said) => {
  console.error(said);
  failing = true;
  serving.kill('SIGTERM');
};

if (!(await serves())) {
  refuse('the factory did not answer GET /healthz on 8090 within two minutes');
} else {
  const read = spawnSync(
    'psql',
    [url, '-t', '-A', '-v', 'ON_ERROR_STOP=1',
      '-c', `select person_key from ${SCHEMA}.people_mapping where name = '${OWNER}' limit 1`],
    { env: quiet, encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] },
  );
  const key = read.status === 0 ? read.stdout.trim() : '';
  if (key === '') {
    refuse(`no per-person key is mapped to ${OWNER}, and the browser run declares that key to act`);
  } else {
    answering = createServer((_, response) => {
      response.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' });
      response.end(key);
    });
    answering.on('error', (cause) => {
      refuse(`nothing can answer the owner's key on 8091: ${cause.message}`);
    });
    answering.listen(8091, '127.0.0.1');
  }
}
