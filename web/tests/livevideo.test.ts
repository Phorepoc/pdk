// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { avcCodec, parseRecords } from '@/api/livevideo';

// one recorder record (lib/device/phlink/rec.go layout)
function rec(frame: number, us: number, flags: number, white: number, body: number[]): Uint8Array {
  const b = new Uint8Array(32 + body.length);
  const dv = new DataView(b.buffer);
  b.set([0x50, 0x52, 0x45, 0x43]);
  dv.setUint32(4, frame, true);
  dv.setBigUint64(8, BigInt(us), true);
  dv.setUint32(16, flags, true);
  dv.setUint16(22, white, true);
  dv.setUint32(24, body.length, true);
  b.set(body, 32);
  return b;
}
const cat = (...a: Uint8Array[]) => { const o = new Uint8Array(a.reduce((n, x) => n + x.length, 0)); let p = 0; for (const x of a) { o.set(x, p); p += x.length; } return o; };

const SPS = [0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x1f, 0xaa];

describe('recorder records', () => {
  it('parses complete records and keeps the incomplete tail', () => {
    const a = rec(0, 0, 1, 12, SPS), b = rec(1, 33333, 0, 990, [0, 0, 0, 1, 0x41, 1, 2]);
    const all = cat(a, b);
    const r = parseRecords(all.subarray(0, all.length - 3));
    expect(r.records.length).toBe(1);
    expect(r.records[0]).toMatchObject({ frame: 0, timeUs: 0, idr: true, end: false, white: 12 });
    expect(r.rest.length).toBe(b.length - 3);
    const r2 = parseRecords(cat(r.rest, all.subarray(all.length - 3)));
    expect(r2.records.map((x) => [x.frame, x.timeUs, x.idr, x.white])).toEqual([[1, 33333, false, 990]]);
    expect(r2.rest.length).toBe(0);
  });
  it('resynchronises after damage and reads the end flag', () => {
    const r = parseRecords(cat(new Uint8Array([9, 9, 9]), rec(5, 7, 2, 0, [])));
    expect(r.records.length).toBe(1);
    expect(r.records[0]?.end).toBe(true);
  });
  it('builds the avc1 codec string from the SPS', () => {
    expect(avcCodec(new Uint8Array(SPS))).toBe('avc1.42c01f');
    expect(avcCodec(new Uint8Array([0, 0, 0, 1, 0x41, 1]))).toBeNull();
  });
});
