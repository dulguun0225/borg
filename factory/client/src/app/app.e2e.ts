// What the shell is decided on in a browser and nowhere a spec fakes the
// factory: that the binary serves this client at all, that the declaration is
// held across a reload and carried on every call, and that the version refusal
// reaches the screen as the required reload above the four.
//
// This file is one of the five the browser run `npm run e2e` drives. A test
// here that decides otherwise fails that run, and the client's build in
// ../../../../.github/workflows/factory.yml fails with it.
//
// What defines it:
// ../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
import { expect, test, type Page } from '@playwright/test';

// Says who the human at the screen is, which is what every call carries. It is
// duplicated in each of the five browser-run files rather than shared: a file
// here imports nothing of the client, and the copies keep one name and one
// spelling so a defect found in one is found in all by one search.
async function declareAs(page: Page, key: string): Promise<void> {
  await page.getByLabel('Acting as').fill(key);
  await page.getByRole('button', { name: 'Declare', exact: true }).click();
}

// The owner's per-person key, which ../../tools/e2e-factory.mjs read from the
// store and answers with. It is the one key an acting call is exempt on: a
// fresh install's declaration is empty, so every other key holds nothing and
// acts nowhere. Duplicated across the five files for the reason above.
async function ownerKey(page: Page): Promise<string> {
  return (await (await page.request.get('http://127.0.0.1:8091/owner')).text()).trim();
}

test('the binary serves the client, and the nav names the four screens', async ({ page }) => {
  await page.goto('/');
  for (const screen of ['Work', 'Ops', 'Factory', 'People']) {
    await expect(page.getByRole('link', { name: screen })).toBeVisible();
  }
});

test('nothing can be read until a human says who they are', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText('Nothing can be read until you say who you are')).toBeVisible();
  await expect(page.getByText('badge unread')).toBeVisible();
});

test('the badge reads once a key is declared, and the key survives a reload', async ({ page }) => {
  const owner = await ownerKey(page);
  await page.goto('/');
  await declareAs(page, owner);
  await expect(page.getByText('0 waiting on a human')).toBeVisible();
  await expect(page.getByText('badge unread')).toBeHidden();

  // The key is in localStorage rather than in a signal alone, so the reload
  // reopens the address as the same human and the badge reads again.
  await page.reload();
  await expect(page.getByLabel('Acting as')).toHaveValue(owner);
  await expect(page.getByText('0 waiting on a human')).toBeVisible();
});

test('a call carrying an earlier factory version is rendered as a required reload', async ({
  page,
}) => {
  await page.goto('/');
  await declareAs(page, await ownerKey(page));
  await expect(page.getByText('0 waiting on a human')).toBeVisible();

  // The server compares X-Factory-Version on every call including a read, so
  // rewriting the header on every call is the whole of what an upgraded
  // factory looks like to a screen that was open across it.
  await page.route('**/api/**', (route) => {
    void route.continue({
      headers: { ...route.request().headers(), 'x-factory-version': 'e2e-stale' },
    });
  });
  await page.goto('/people');
  await expect(
    page.getByRole('heading', { name: 'This screen was built for an earlier factory' }),
  ).toBeVisible();
});
