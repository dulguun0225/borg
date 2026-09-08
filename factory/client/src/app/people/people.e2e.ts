// What People is decided on in a browser and nowhere a spec fakes the factory:
// that a write reaching a record the factory owns is carried back to a screen
// that did not make it, and that the screen says so when its subscription to
// the address is down.
//
// The write is made from outside the page, as a call to the same address the
// form posts to, while the page sits idle on /people. That is what makes the
// row appearing a reading of the subscription: the screen re-reads what it
// sends itself, so a row that followed a click here would render whether the
// subscription carried anything or not.
//
// The declaration is never empty here: the owner's row is the view's own and
// comes from no record, so what a fresh install shows is that row alone. The
// duty this file declares is on a second key, and it is withdrawn after each
// test whether or not the test reached its end, so the declaration is left as
// it was found.
//
// This file is one of the five the browser run `npm run e2e` drives. A test
// here that decides otherwise fails that run, and the client's build in
// ../../../../../.github/workflows/factory.yml fails with it.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
import { expect, test, type Page } from '@playwright/test';

// Says who the human at the screen is, which is what every call carries. It is
// duplicated in each of the five browser-run files rather than shared: a file
// here imports nothing of the client, and the copies keep one name and one
// spelling so a defect found in one is found in all by one search.
async function declareAs(page: Page, key: string): Promise<void> {
  await page.getByLabel('Acting as').fill(key);
  await page.getByRole('button', { name: 'Declare', exact: true }).click();
}

// The owner's per-person key, read from the People view the way a human at the
// screen reads it: the owner is that view's first row, and theirs is the one
// key an acting call is exempt on — every other key holds nothing on a fresh
// install and acts nowhere. A read is refused for no principal and for no
// factory version, and for nothing else, so any key at all opens this one.
// Duplicated across the five files for the reason above.
async function ownerKey(page: Page): Promise<string> {
  const version = (await (await page.request.get('/healthz')).text()).trim();
  const answer = await page.request.get('/api/people', {
    headers: { 'X-Factory-Version': version, 'X-Factory-Principal': 'e2e-reader' },
  });
  const view = (await answer.json()) as { Rows: { Key: string; Owner: boolean }[] };
  return view.Rows.find((row) => row.Owner)?.Key ?? '';
}

// The key the duty is declared on. It is not the owner's: the owner's row is
// there before anything is declared, so a row appearing is only a reading of
// the subscription where the row is one that was not there.
const SECOND = 'e2e-second';

const READY = 'Kept true by the subscription on this address.';

// One call to POST /api/call/{name}, made from outside the page and carrying
// the two headers every call carries. The factory version is read from
// /healthz, which is the one route a reader outside the process compares it
// on, rather than written down a second time here.
async function call(page: Page, name: string, args: object): Promise<number> {
  const version = (await (await page.request.get('/healthz')).text()).trim();
  const answer = await page.request.post(`/api/call/${name}`, {
    headers: {
      'Content-Type': 'application/json',
      'X-Factory-Version': version,
      'X-Factory-Principal': await ownerKey(page),
    },
    data: args,
  });
  return answer.status();
}

// Runs whether or not a test reached its end, so a failure part-way through
// leaves no row for the next file to read. The call's own answer is not
// asserted on: where the test failed before it declared anything, there is no
// holding standing to withdraw and the factory refuses it. What is asserted is
// the state the screen is left in, which is the same either way.
test.afterEach(async ({ page }) => {
  await call(page, 'withdrawDuty', { HumanKey: SECOND, Duty: 1 });
  await expect(page.getByText(`Key ${SECOND}`)).toBeHidden();
});

test('a duty declared elsewhere reaches the screen over its subscription', async ({ page }) => {
  // The key is declared in the shell and the address is opened after it, which
  // is the order a human works in.
  await page.goto('/');
  await declareAs(page, await ownerKey(page));
  await page.goto('/people');
  // The owner's row, which the view holds before anything is declared and
  // which is where the key every call carries is read from.
  await expect(page.getByText('This row is the owner this install was made as, and it acts everywhere though it holds no duty and lends nothing.')).toBeVisible();
  await expect(page.getByText(`Key ${SECOND}`)).toBeHidden();

  // Nothing is clicked and nothing is typed. The page is idle on /people, the
  // call is made beside it, and the row renders because the subscription on
  // this address reported the change and the screen re-read it.
  expect(await call(page, 'declareDuty', { HumanKey: SECOND, Duty: 1 })).toBe(204);

  await expect(page.getByText(`Key ${SECOND}`)).toBeVisible();
  await expect(page.getByText(READY)).toBeVisible();
});

test('the screen says so when the subscription to this address drops', async ({ page }) => {
  await page.goto('/');
  await declareAs(page, await ownerKey(page));
  await page.goto('/people');
  await expect(page.getByText(READY)).toBeVisible();

  // The reader owns the EventSource, so nothing the page evaluates can close
  // it. What re-opens one from outside the reader is a declaration — every
  // holder is told and closes what it has — so refusing the subscription and
  // declaring again is a subscription that is down, which is what the screen
  // reports.
  await page.route('**/api/stream/**', (route) => {
    void route.abort();
  });
  await declareAs(page, await ownerKey(page));
  await expect(page.getByText('The subscription to this address has dropped')).toBeVisible();

  await page.unroute('**/api/stream/**');
  await declareAs(page, await ownerKey(page));
  await expect(page.getByText(READY)).toBeVisible();
});
