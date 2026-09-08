# The client

The four screens the factory serves, as one Angular application. Its build output is
`../clientdist/browser/`, which `../clientdist/embed.go` embeds with the standard
library's `embed`: one binary serves the screens, and no client runs apart from the
factory it was built with.

## The profile

One dialect and nothing outside it. The reason the client is Angular at all is what the
build can refuse to ship — a misspelled field, a wrong type in a binding, and a form
model out of step with its template are build errors here.

- Standalone components; signals and `computed` for state.
- Zoneless. `provideZonelessChangeDetection()` is declared in `src/app/app.config.ts`
  rather than left to the framework's default, and `zone.js` is not a dependency.
- Strict TypeScript — `strict`, `noUncheckedIndexedAccess`,
  `exactOptionalPropertyTypes` — and strict templates: `strictTemplates`,
  `strictInjectionParameters`, `strictInputAccessModifiers`.
- Signal Forms as the one form dialect, imported from `@angular/forms/signals`. It is
  public API in this version, not experimental. The directive that binds a control is
  `FormField`, used as `[formField]="someForm.someField"`, and it sets the control's
  `name` itself — setting `name` beside it is a compile error.
- No RxJS in application code. The framework depends on it and application code never
  imports it, which the lint wall refuses. A route parameter reaches a component as a
  signal input through `withComponentInputBinding()` rather than as an observable.
- A hand-written `fetch` client and the browser's `EventSource`, both in `src/app/api/`.
- Not in it: Material, a grid library, `$localize`, and any component library at all.
  The screens are semantic HTML against `src/tokens.css` and `src/styles.css`, in one
  language until an owner supplies another.

## The layout

One directory per concept. `src/app/api/` is every call to the factory, `src/app/state/`
is the state machine type the four screens declare and the four predicates their specs
decide, and each of `src/app/work/`, `src/app/ops/`, `src/app/factory/` and
`src/app/people/` is one screen. Each screen directory holds the same file names —
`README.md`, `<screen>.ts`, `<screen>.html`, `<screen>.css`, `<screen>.spec.ts`,
`<screen>.e2e.ts`, `<screen>.routes.ts`, `format.ts` — and Work, Ops and Factory each hold
one further file pair per address under them. Each screen's `README.md` says what it owns
and names the `end-goal/` file it implements, which is this client's counterpart to a
package's `doc.go`.

`format.ts` is one file duplicated across the four screen directories, and `request.ts` is
a second such copy, in the two screen directories that hold one. A screen imports `api/`
and `state/` and nothing else outside its own directory, so a helper shared between two
screens would be an edge the boundary refuses; the repetition is the cheaper of the two,
and the copies keep one name and one spelling so a defect found in one is found in all by
one search.

Factory and People are each too large for one component under the 500-line bound, so each
is split into sections in its own directory — `factory-fleet.ts`, `factory-policy.ts`,
`factory-places.ts`, `factory-numbers.ts`, `people-forms.ts` — named with the `Section`
suffix `eslint.config.js` allows beside `Screen` and `Shell`. A section is not a fifth
screen: it holds no address, no subscription and no state machine, and it performs no call.
It emits the call it wants made, and the screen holds the one path every call takes.

## The import boundary

`eslint.config.js` holds this client's counterpart to `../deps.txt`: one allowed edge
per block with the reason beside it, as a `no-restricted-imports` pattern that fails
`npm run lint`. The edges are:

- a screen imports `api/` and `state/`;
- a screen's spec additionally imports the fakes under `src/testing/`;
- `api/` and `state/` import nothing of the app, except `src/environments/version.ts`;
- the shell imports the four screens' routes and nothing else of a screen;
- an end-to-end file imports nothing of the app: it reaches the client through the
  browser, over HTTP, the way a human does;
- nothing under `src/app/`, `src/main.ts`, `src/testing/`, or
  `src/environments/` imports `rxjs`, `zone.js`, `@angular/localize`, or
  `@angular/material`.

## The version

`src/environments/version.ts` holds `FACTORY_VERSION`, the factory version this client
was built from, sent as `X-Factory-Version` on every call including a read. The server
refuses a call whose version is not its own, and the client renders that refusal as a
required reload and never as a failed action. The constant must equal `factoryVersion`
in `../cmd/factory/main.go`; `npm run set-version` copies it across. That one constant
is the whole of what a script writes into this source tree.

## Commands

Run these from `factory/client/`.

| Command | What it does |
|---|---|
| `npm ci` | Installs exactly the lockfile. Every version in `package.json` is pinned exact |
| `npm run set-version` | Copies `factoryVersion` from `../cmd/factory/main.go` into `src/environments/version.ts` |
| `npm run lint` | The lint wall, over `src/**/*.ts`, `src/**/*.html` and `playwright.config.ts` |
| `npm test` | One headless run of every spec |
| `npm run e2e` | The browser run: the shell and the four screens driven in Chromium against a factory this commit builds. It needs the client built, `psql` on the path, a reachable `DATABASE_URL`, and port 8090 free. Two runs on one `DATABASE_URL` cannot overlap: both use the schema `factory_e2e`, and the second drops it under the first |
| `npm run build` | Builds into `../clientdist/browser/`, then puts `.gitkeep` back |
| `npm start` | The development server, for a screen served without the factory |

`npm run build` writes into `../clientdist/browser/`, which the Angular build empties
first — so the `postbuild` step puts `browser/.gitkeep` back, that file being what lets
the directory embed on a fresh clone whose client has not been built. Everything else
under `browser/` is excluded by the repository's `.gitignore`.

## The tests

`npm test` is the `@angular/build:karma` builder with Jasmine, in one headless Chrome,
`singleRun` through the builder's `watch: false`. The runner is a browser and not jsdom
because the four predicates below are decided over resolved styles and a real layout,
which jsdom answers for neither: it resolves no `var()` in a computed style and lays
nothing out, so a contrast floor and a target size would both pass vacuously. The
launcher is `ChromeHeadlessNoSandbox` in `karma.conf.js` — a container image and a
hosted runner both refuse to enter Chrome's sandbox. `CHROME_BIN` is read by
`karma-chrome-launcher` where the browser is not on the default path.

Every call blocks the application's reported stability while it is in flight, through
`PendingTasks` in `src/app/api/client.ts`. That is what makes `fixture.whenStable()`
wait for a read rather than resolve before it, so no spec sleeps.

Each screen's spec drives its component into every state the screen's machine declares
and decides the four predicates a run on a candidate environment decides over each
rendered state: a contrast floor of 4.5:1 computed from the resolved tokens, an
accessible name on every control, a focus order with no positive `tabindex` and every
tabbable element able to take focus, and a target size of at least 24×24 CSS pixels.
They are in `src/app/state/predicates.ts` so all four screens decide the same four.

`npm run e2e` is the second run and drives what a spec cannot: `playwright.config.ts`
starts `tools/e2e-factory.mjs`, which builds the binary of this commit and serves the
client this build embedded on 8090, and five files drive that in one headless Chromium.
`src/app/app.e2e.ts` decides the shell's part — that a screen is served by the process,
that the declaration is carried on every call and held across a reload, and that the
version refusal is rendered as the required reload — and each `<screen>.e2e.ts` decides
its own screen's, `people.e2e.ts` among them deciding that a write reaching a record is
carried back to a screen that did not make it.

The store is the schema `factory_e2e`: dropped and created before the process starts, so
every run begins on an empty one, and left in the database when the run ends. A run is
told to use a model name no provider answers, and nothing dispatches an agent unless an
intent is supplied — no test supplies one, which is what keeps the run away from a
provider.

The key a test declares at the top of a screen is the owner's, and each file reads it
from the People view the way a human does: that view holds the owner's row before
anything is declared, and the owner's is the one key an acting call is exempt on.

## What defines it

[The screens as software](../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md);
[Work, Ops, Factory, People](../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md);
[Three properties every screen needs](../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md);
[The screen state machine](../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md);
[The third outcome](../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md).
