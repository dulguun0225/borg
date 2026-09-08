// The browser run's one configuration. What it drives is the factory's own
// process — tools/e2e-factory.mjs builds the binary of this commit, gives it a
// store of the run's own, and serves the client this build embedded — so what
// a test decides is decided over the two together and not over a fake of
// either.
//
// The runner is a browser for the reason the client is Angular at all: the
// screens are served by the binary, the declaration is held in localStorage,
// the subscription is an EventSource, and the version refusal is a header on
// every call. None of the four is answered outside one.
//
// One worker and no parallelism: the run writes records, the factory holds one
// lease, and two processes on one store is the state the lease exists to
// prevent. Retries are zero for the same reason — a test that passed only on
// its second run passed against records the first one wrote.
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: 'src/app',
  testMatch: '**/*.e2e.ts',
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: 'list',
  outputDir: 'test-results',
  use: {
    baseURL: 'http://127.0.0.1:8090',
    headless: true,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'node tools/e2e-factory.mjs',
    // The harness's own route rather than the factory's /healthz: it answers
    // only once the factory serves and the owner's per-person key has been
    // read, and a test that acts declares that key.
    url: 'http://127.0.0.1:8091/owner',
    reuseExistingServer: false,
    // Above the harness's own two-minute wait on the factory, so a factory
    // that never serves is reported in the harness's words; what the rest of
    // this budget is for is the go build the harness runs first.
    timeout: 300000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
