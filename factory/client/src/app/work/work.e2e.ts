// What Work is decided on in a browser and nowhere a spec fakes the factory:
// that the screen the binary serves reads the home address as the declared
// human and renders what the factory's own records hold.
//
// It is opened before anybody says who they are, which is what a human lands
// on: the first read is refused for want of a key, and the declaration made
// from the screen itself is what has to bring it back. That is the whole of
// what a subscription re-established does, and the screen it recovers is not
// a screen this test opened again.
//
// A fresh install is not the screen's empty state. The home view carries a
// readiness reading per role from the moment the fleet is written, and Work's
// empty predicate reads that beside the rows — so the screen is ready, the
// badge reads zero, and the two actions the empty state offers are rendered
// from the body instead.
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

test('Work recovers from its refused first read when a key is declared', async ({ page }) => {
  const owner = await ownerKey(page);
  await page.goto('/work');
  await expect(page.getByText('What waits on a human could not be read')).toBeVisible();

  // Nothing here reloads and nothing navigates. The declaration re-establishes
  // the subscription on this address, and re-establishing one re-reads the
  // address whole.
  await declareAs(page, owner);

  await expect(page.getByText('What waits on a human could not be read')).toBeHidden();
  await expect(page.getByRole('heading', { name: 'Waiting on a human: 0' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Supply an intent' })).toBeVisible();
});
