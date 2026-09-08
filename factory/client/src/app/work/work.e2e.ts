// What Work is decided on in a browser and nowhere a spec fakes the factory:
// that the screen the binary serves reads the home address as the declared
// human and renders what the factory's own records hold.
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

// The owner's per-person key, which ../../../tools/e2e-factory.mjs read from
// the store and answers with. It is the one key an acting call is exempt on: a
// fresh install's declaration is empty, so every other key holds nothing and
// acts nowhere. Duplicated across the five files for the reason above.
async function ownerKey(page: Page): Promise<string> {
  return (await (await page.request.get('http://127.0.0.1:8091/owner')).text()).trim();
}

test('Work reads the home address and offers the way an intent is supplied', async ({ page }) => {
  // The key is declared in the shell and the address is opened after it, which
  // is the order a human works in: a screen already open when the declaration
  // is made holds the failed read it started with until it is read again.
  await page.goto('/');
  await declareAs(page, await ownerKey(page));
  await page.goto('/work');

  await expect(page.getByRole('heading', { name: 'Waiting on a human: 0' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Supply an intent' })).toBeVisible();
});
