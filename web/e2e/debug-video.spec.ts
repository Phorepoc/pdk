// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Diagnostic: the live video element's state over time (run alone: npx playwright test debug-video).
import { test } from '@playwright/test';

const URL0 = process.env.PHONELAB_URL ?? 'http://127.0.0.1:8737';
const DEV = process.env.PHONELAB_DEVICE ?? 'pinephone';

test('debug video state', async ({ page }) => {
  test.setTimeout(60000);
  page.on('console', (m) => console.log('console:', m.type(), m.text()));
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Media.enable');
  cdp.on('Media.playerMessagesLogged', (e) => { for (const m of e.messages) console.log('MEDIA msg', m.level, m.message.slice(0, 300)); });
  cdp.on('Media.playerErrorsRaised', (e) => { for (const m of e.errors) console.log('MEDIA err', JSON.stringify(m).slice(0, 400)); });
  cdp.on('Media.playerPropertiesChanged', (e) => { for (const m of e.properties) if (/error|decoder|codec|state/i.test(m.name)) console.log('MEDIA prop', m.name, String(m.value).slice(0, 200)); });
  await page.addInitScript(([dev]) => {
    localStorage.setItem('phonelab.device', dev as string);
    localStorage.setItem('phonelab.remote.v1', JSON.stringify({ source: 'video', which: 2, zoom: 'fit', keypadOpen: false }));
  }, [DEV]);
  await page.addInitScript(() => {
    const w = window as unknown as { __sb: string[] };
    w.__sb = [];
    const ap = SourceBuffer.prototype.appendBuffer;
    SourceBuffer.prototype.appendBuffer = function (d: BufferSource) {
      const sb = this as SourceBuffer;
      if (!(sb as unknown as { __h?: boolean }).__h) {
        (sb as unknown as { __h?: boolean }).__h = true;
        sb.addEventListener('error', () => w.__sb.push('sb error'));
        sb.addEventListener('updateend', () => { const b = sb.buffered; w.__sb.push('end ' + (b.length ? b.end(b.length - 1).toFixed(2) : '-')); });
      }
      w.__sb.push('append ' + (d as ArrayBuffer).byteLength + ' mode=' + sb.mode);
      try { return ap.call(this, d); } catch (e) { w.__sb.push('throw ' + (e as Error).message); throw e; }
    };
    const as = MediaSource.prototype.addSourceBuffer;
    MediaSource.prototype.addSourceBuffer = function (t: string) { w.__sb.push('addSourceBuffer ' + t); return as.call(this, t); };
  });
  await page.goto(URL0 + '/');
  for (let i = 0; i < 12; i++) {
    await page.waitForTimeout(1000);
    if (i === 4) await page.evaluate(async ([u, d]) => { await fetch(`${u}/api/devices/${d}/ops/key`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"key":"menu"}' }); }, [URL0, DEV]);
    if (i === 8) await page.evaluate(async ([u, d]) => { await fetch(`${u}/api/devices/${d}/ops/key`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"key":"menu"}' }); }, [URL0, DEV]);
    console.log(i, await page.evaluate(() => {
      const v = document.querySelector('video') as HTMLVideoElement | null;
      if (!v) return 'no video element';
      const b = v.buffered; const r = [];
      for (let k = 0; k < b.length; k++) r.push(`${b.start(k).toFixed(2)}-${b.end(k).toFixed(2)}`);
      const q = (v as unknown as { getVideoPlaybackQuality?: () => VideoPlaybackQuality }).getVideoPlaybackQuality?.();
      return `rs=${v.readyState} paused=${v.paused} t=${v.currentTime.toFixed(2)} buf=[${r.join(' ')}] err=${v.error?.message ?? ''} ` +
        `frames=${q?.totalVideoFrames} dropped=${q?.droppedVideoFrames} size=${v.videoWidth}x${v.videoHeight} stats=${document.querySelector('[data-test=stats]')?.textContent?.trim()}`;
    }));
  }
  const log = await page.evaluate(() => (window as unknown as { __sb: string[] }).__sb);
  console.log('SB', log.length, JSON.stringify(log.slice(0, 30)));
  console.log('SB tail', JSON.stringify(log.slice(-12)));
});
