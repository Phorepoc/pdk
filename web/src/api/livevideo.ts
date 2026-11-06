// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

/** The live video view: the phone's screen recorder (record_start) decoded in the browser. record_peek hands out the
 *  bytes PhoneLab has already collected for the running recording (no phone traffic of its own), a run of records
 *  {"PREC", frame u32, time us u64, flags u32 (1 = IDR, 2 = end), mean luma u16, white permille u16, length u32,
 *  reserved u32} + an Annex B access unit (lib/device/phlink/rec.go); WebCodecs' VideoDecoder decodes them. */

export interface RecRecord { frame: number; timeUs: number; idr: boolean; end: boolean; white: number; body: Uint8Array }

const HEAD = 32;
const MAGIC = [0x50, 0x52, 0x45, 0x43]; // "PREC"

function isMagic(b: Uint8Array, p: number): boolean {
  return b[p] === MAGIC[0] && b[p + 1] === MAGIC[1] && b[p + 2] === MAGIC[2] && b[p + 3] === MAGIC[3];
}

/** Splits a byte run into complete records; `rest` = the incomplete tail to prepend to the next run. */
export function parseRecords(b: Uint8Array): { records: RecRecord[]; rest: Uint8Array } {
  const out: RecRecord[] = [];
  const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  let p = 0;
  while (p + HEAD <= b.length) {
    if (!isMagic(b, p)) { p++; continue; } // damage: resynchronise on the next record
    const n = dv.getUint32(p + 24, true);
    if (p + HEAD + n > b.length) break;
    const flags = dv.getUint32(p + 16, true);
    out.push({
      frame: dv.getUint32(p + 4, true),
      timeUs: Number(dv.getBigUint64(p + 8, true)),
      idr: (flags & 1) !== 0,
      end: (flags & 2) !== 0,
      white: dv.getUint16(p + 22, true),
      body: b.subarray(p + HEAD, p + HEAD + n),
    });
    p += HEAD + n;
  }
  return { records: out, rest: b.slice(p) };
}

/** The WebCodecs codec string (avc1.PPCCLL) from the first SPS in an Annex B access unit, or null. */
export function avcCodec(au: Uint8Array): string | null {
  for (let i = 0; i + 4 < au.length; i++) {
    if (au[i] === 0 && au[i + 1] === 0 && au[i + 2] === 1 && ((au[i + 3] ?? 0) & 0x1f) === 7) {
      const h = (x: number) => x.toString(16).padStart(2, '0');
      return `avc1.${h(au[i + 4] ?? 0)}${h(au[i + 5] ?? 0)}${h(au[i + 6] ?? 0)}`;
    }
  }
  return null;
}

function b64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export interface LiveVideoOpts {
  /** record_peek: resolves with its result ({bytes: base64, from, next, recording}) */
  peek: (from?: number) => Promise<{ bytes?: string; next?: number; recording?: boolean } | undefined>;
  canvas: () => HTMLCanvasElement | null;
  onFrame: (w: number, h: number, white: number) => void;
  onError: (msg: string) => void;
  onEnded: () => void;
}

export const liveVideoSupported = (): boolean => typeof (globalThis as { VideoDecoder?: unknown }).VideoDecoder === 'function';

export class LiveVideo {
  private dec: VideoDecoder | null = null;
  private next: number | undefined;
  private rest: Uint8Array = new Uint8Array(0);
  private timer: ReturnType<typeof setTimeout> | undefined;
  private running = false;
  private lastWhite = 0;

  constructor(private o: LiveVideoOpts) {}

  get active(): boolean { return this.running; }

  start(): void {
    if (this.running) return;
    this.running = true;
    this.next = undefined; // the first peek starts at the last IDR
    this.rest = new Uint8Array(0);
    void this.tick();
  }

  stop(): void {
    this.running = false;
    clearTimeout(this.timer);
    if (this.dec && this.dec.state !== 'closed') this.dec.close();
    this.dec = null;
  }

  private async tick(): Promise<void> {
    if (!this.running) return;
    let more = false;
    try {
      const r = await this.o.peek(this.next);
      if (!this.running) return;
      const raw = r?.bytes ? b64(r.bytes) : new Uint8Array(0);
      if (r?.next !== undefined) {
        more = this.next !== undefined && r.next - this.next >= 2 << 20; // a full reply: catch up at once
        this.next = r.next;
      }
      if (raw.length) this.feed(raw);
      if (r && !r.recording) { this.stop(); this.o.onEnded(); return; }
    } catch (e) {
      this.o.onError((e as Error).message);
    }
    if (this.running) this.timer = setTimeout(() => void this.tick(), more ? 0 : 150);
  }

  private feed(raw: Uint8Array): void {
    const all = new Uint8Array(this.rest.length + raw.length);
    all.set(this.rest);
    all.set(raw, this.rest.length);
    const { records, rest } = parseRecords(all);
    this.rest = rest;
    for (const rec of records) {
      if (rec.end || !rec.body.length) continue;
      if (!this.dec) {
        if (!rec.idr) continue; // a stream decodes from an IDR
        const codec = avcCodec(rec.body);
        if (!codec) continue;
        this.dec = new VideoDecoder({
          output: (f) => this.draw(f),
          error: (e) => { this.o.onError(`decoder: ${e.message}`); this.dec = null; },
        });
        this.dec.configure({ codec, optimizeForLatency: true });
      }
      if (this.dec.state !== 'configured') continue;
      this.lastWhite = rec.white;
      this.dec.decode(new EncodedVideoChunk({ type: rec.idr ? 'key' : 'delta', timestamp: rec.timeUs, data: rec.body }));
    }
  }

  private draw(f: VideoFrame): void {
    const c = this.o.canvas();
    if (c) {
      if (c.width !== f.displayWidth || c.height !== f.displayHeight) { c.width = f.displayWidth; c.height = f.displayHeight; }
      c.getContext('2d')?.drawImage(f, 0, 0);
      this.o.onFrame(f.displayWidth, f.displayHeight, this.lastWhite);
    }
    f.close();
  }
}
