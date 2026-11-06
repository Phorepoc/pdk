// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

/** The live view's video transport: the phone's screen encoder as fragmented MP4 (op live_video), played by the
 *  browser's own <video> through Media Source Extensions -- works on plain http (no WebCodecs, no secure context).
 *  Records nothing: each live_video call only keeps the encoder running for a few more seconds. PhoneLab paces each
 *  viewer to a constant 30 pictures a second (repeats of the last picture fill the encoder's gaps) on a continuous
 *  timeline: a gap reads as a discontinuity to MSE, which then drops everything up to the next IDR, and media time
 *  slower than the clock stalls the player. It keeps up by playing faster while behind -- never by seeking (a seek
 *  decodes again from the last IDR). */

export interface LiveVideoReply {
  init?: string; frag?: string; codec?: string; reset?: boolean; waiting?: boolean;
  next?: number; width?: number; height?: number; white?: number; recording?: boolean;
}

export const liveVideoSupported = (): boolean =>
  typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported('video/mp4; codecs="avc1.42c01f"');

function b64(s: string): Uint8Array<ArrayBuffer> {
  const bin = atob(s);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export interface LiveVideoOpts {
  call: (from?: number) => Promise<LiveVideoReply | undefined>;
  video: () => HTMLVideoElement | null;
  onFrame: (w: number, h: number, white: number) => void;
  onError: (msg: string) => void;
}

export class LiveVideo {
  private running = false;
  private next: number | undefined;
  private ms: MediaSource | null = null;
  private sb: SourceBuffer | null = null;
  private url = '';
  private queue: Uint8Array<ArrayBuffer>[] = [];
  private timer: ReturnType<typeof setTimeout> | undefined;
  private size = { w: 0, h: 0 };
  private appended = 0;

  constructor(private o: LiveVideoOpts) {}

  get active(): boolean { return this.running; }

  start(): void {
    if (this.running) return;
    this.running = true;
    this.next = undefined;
    void this.tick();
  }

  stop(): void {
    this.running = false;
    clearTimeout(this.timer);
    this.teardown();
  }

  private teardown(): void {
    this.queue = [];
    this.sb = null;
    if (this.ms && this.ms.readyState === 'open') { try { this.ms.endOfStream(); } catch { /* closing */ } }
    this.ms = null;
    const v = this.o.video();
    if (v) { v.removeAttribute('src'); v.load(); }
    if (this.url) URL.revokeObjectURL(this.url);
    this.url = '';
  }

  private async tick(): Promise<void> {
    if (!this.running) return;
    let delay = 60;
    try {
      const r = await this.o.call(this.next);
      if (!this.running) return;
      if (r?.waiting) { this.next = undefined; delay = 400; }
      else if (r) {
        if (r.reset && r.init && r.codec) await this.open(r.codec, b64(r.init));
        if (r.width && r.height) this.size = { w: r.width, h: r.height };
        if (r.frag) this.push(b64(r.frag), r.white ?? 0);
        this.next = r.next;
      }
    } catch (e) {
      this.o.onError((e as Error).message);
      delay = 1000;
    }
    if (this.running) this.timer = setTimeout(() => void this.tick(), delay);
  }

  private open(codec: string, init: Uint8Array<ArrayBuffer>): Promise<void> {
    this.teardown();
    const v = this.o.video();
    if (!v) return Promise.resolve();
    const ms = new MediaSource();
    this.ms = ms;
    this.url = URL.createObjectURL(ms);
    return new Promise((resolve) => {
      ms.addEventListener('sourceopen', () => {
        try {
          const sb = ms.addSourceBuffer(`video/mp4; codecs="${codec}"`);
          sb.mode = 'segments';
          sb.addEventListener('updateend', () => this.pump());
          this.sb = sb;
          this.queue.push(init);
          this.pump();
        } catch (e) { this.o.onError(`video: ${(e as Error).message}`); }
        resolve();
      }, { once: true });
      v.src = this.url;
      v.muted = true;
      void v.play().catch(() => { /* starts on the first data */ });
    });
  }

  private push(frag: Uint8Array<ArrayBuffer>, white: number): void {
    this.queue.push(frag);
    this.pump();
    this.o.onFrame(this.size.w, this.size.h, white);
  }

  private pump(): void {
    const sb = this.sb, v = this.o.video();
    if (!sb || sb.updating || !v) return;
    // keep the newest picture on screen: play faster while behind; seek only when hopelessly behind
    const b = sb.buffered;
    if (b.length) {
      const end = b.end(b.length - 1);
      const behind = end - v.currentTime;
      if (behind > 3) v.currentTime = end - 0.1;
      else v.playbackRate = behind > 0.4 ? 1.5 : behind > 0.15 ? 1.1 : 1; // gently: a big jump runs dry and rebuffers
      if (v.paused) void v.play().catch(() => {});
      if (++this.appended % 300 === 0 && v.currentTime - b.start(0) > 20) { sb.remove(b.start(0), v.currentTime - 10); return; }
    }
    const next = this.queue.shift();
    if (!next) return;
    try { sb.appendBuffer(next); } catch (e) { this.o.onError(`video: ${(e as Error).message}`); this.next = undefined; }
  }
}
