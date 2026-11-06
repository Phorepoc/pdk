// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Live view against a RUNNING PhoneLab with a phone (not part of `npm test`): input-to-picture latency and the pictures
// actually shown, for each source. Run: PHONELAB_URL=http://127.0.0.1:8737 PHONELAB_DEVICE=pinephone npx playwright test
// (Edge or Chrome: Playwright's own Chromium has no H.264.)
import { expect, test, type Page } from '@playwright/test';

const URL0 = process.env.PHONELAB_URL ?? 'http://127.0.0.1:8737';
const DEV = process.env.PHONELAB_DEVICE ?? 'pinephone';

interface Result { latencyMs: number; distinct2s: number; presented2s: number }

async function measure(page: Page, source: 'shots' | 'video', interval: number): Promise<Result> {
  await page.addInitScript(([dev, src, iv]) => {
    localStorage.setItem('phonelab.device', dev as string);
    localStorage.setItem('phonelab.remote.v1', JSON.stringify({ source: src, intervalS: iv, which: 2, zoom: 'fit', keypadOpen: false }));
  }, [DEV, source, interval]);
  await page.goto(URL0 + '/');
  const sel = source === 'video' ? 'video.preview-img' : 'img.preview-img';
  await page.waitForSelector(sel, { state: 'visible', timeout: 30000 });
  // sample every picture the page presents: (time, hash of a 36x72 thumbnail)
  await page.evaluate((s) => {
    const w = window as unknown as { __pics: [number, number][] };
    w.__pics = [];
    const c = document.createElement('canvas'); c.width = 36; c.height = 72;
    const g = c.getContext('2d', { willReadFrequently: true })!;
    const hash = (el: CanvasImageSource) => {
      g.drawImage(el, 0, 0, 36, 72);
      const d = g.getImageData(0, 0, 36, 72).data; let h = 0;
      for (let i = 0; i < d.length; i += 4) h = (h * 31 + (d[i]! >> 3) + (d[i + 1]! >> 3) * 7 + (d[i + 2]! >> 3) * 13) | 0;
      return h;
    };
    if (s === 'video') {
      const v = document.querySelector('video.preview-img') as HTMLVideoElement;
      const cb = () => { w.__pics.push([performance.now(), hash(v)]); v.requestVideoFrameCallback(cb); };
      v.requestVideoFrameCallback(cb);
    } else {
      const seen = new WeakSet<Element>();
      const watch = () => {
        const im = document.querySelector('img.preview-img') as HTMLImageElement | null;
        if (im && !seen.has(im)) { seen.add(im); im.addEventListener('load', () => w.__pics.push([performance.now(), hash(im)])); }
      };
      new MutationObserver(watch).observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['src'] });
      watch();
    }
  }, source);
  await page.waitForTimeout(4000);
  const key = (k: string) => page.evaluate(async ([u, d, kk]) => {
    const t = performance.now();
    await fetch(`${u}/api/devices/${d}/ops/key`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ key: kk }) });
    return t;
  }, [URL0, DEV, k]);
  const t0 = await key('menu');
  await page.waitForTimeout(4000);
  await key('menu'); // back to the home screen
  await page.waitForTimeout(2500);
  const pics = await page.evaluate(() => (window as unknown as { __pics: [number, number][] }).__pics);
  const before = [...pics].reverse().find((p) => p[0] < t0);
  const first = pics.find((p) => p[0] >= t0 && (!before || p[1] !== before[1]));
  const latencyMs = first ? first[0] - t0 : Infinity;
  const win = first ? pics.filter((p) => p[0] >= first[0] && p[0] < first[0] + 2000) : [];
  const distinct2s = new Set(win.map((p) => p[1])).size;
  return { latencyMs: Math.round(latencyMs), distinct2s, presented2s: win.length };
}

test('live view: screenshots vs video', async ({ browser }) => {
  test.setTimeout(120000);
  const res: Record<string, Result> = {};
  for (const [name, src, iv] of [['shots 0.2s', 'shots', 0.2], ['video', 'video', 1]] as const) {
    const ctx = await browser.newContext();
    res[name] = await measure(await ctx.newPage(), src, iv);
    await ctx.close();
    await new Promise((r) => setTimeout(r, 7000)); // the encoder lease runs out between runs
  }
  console.log(JSON.stringify(res, null, 1));
  // video must beat screenshots: sooner and smoother
  expect(res.video!.latencyMs).toBeLessThan(res['shots 0.2s']!.latencyMs);
  expect(res.video!.distinct2s).toBeGreaterThan(res['shots 0.2s']!.distinct2s);
});
