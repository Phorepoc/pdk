// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Diagnostic: a picture of the header bar (npx playwright test shot-header).
import { test } from '@playwright/test';

test('header picture', async ({ page }) => {
  await page.goto((process.env.PHONELAB_URL ?? 'http://127.0.0.1:8737') + '/');
  await page.waitForSelector('.ide-twhead .q-select');
  await page.locator('.ide-twhead').first().screenshot({ path: 'test-results/header.png' });
  const r = await page.evaluate(() => {
    const q = (s: string) => { const e = document.querySelector(s); if (!e) return null; const b = e.getBoundingClientRect(); return [Math.round(b.x), Math.round(b.y), Math.round(b.width), Math.round(b.height)]; };
    return { bar: q('.ide-twhead'), select: q('.ide-twhead .q-select'), control: q('.ide-twhead .q-field__control'), native: q('.ide-twhead .q-field__native'), arrow: q('.ide-twhead .q-select__dropdown-icon') };
  });
  console.log(JSON.stringify(r));
});
