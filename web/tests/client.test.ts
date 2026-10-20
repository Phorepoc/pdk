// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, onUnauthorized, setToken } from '@/api/client';

function reply(status: number, body: unknown): Response {
  return new Response(typeof body === 'string' ? body : JSON.stringify(body), { status });
}

describe('api client', () => {
  const fetchMock = vi.fn();
  beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset(); setToken(''); onUnauthorized(null); });
  afterEach(() => vi.unstubAllGlobals());

  it('POSTs op args as JSON with the caller header and unwraps result', async () => {
    fetchMock.mockResolvedValue(reply(200, { result: [{ addr: '0x1', value: '0x2' }] }));
    const r = await api.op('pinephone', 'peek', { addr: '0x01c20000', count: 4 });
    expect(r).toEqual([{ addr: '0x1', value: '0x2' }]);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/devices/pinephone/ops/peek');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ addr: '0x01c20000', count: 4 });
    const h = init.headers as Record<string, string>;
    expect(h['X-Phonelab-Caller']).toBe('web');
    expect(h['Authorization']).toBeUndefined();
  });

  it('sends the bearer token when set', async () => {
    setToken('  s3cret ');
    fetchMock.mockResolvedValue(reply(200, []));
    await api.devices();
    const h = (fetchMock.mock.calls[0] as [string, RequestInit])[1].headers as Record<string, string>;
    expect(h['Authorization']).toBe('Bearer s3cret');
    expect(api.blobUrl('abc')).toBe('/api/blobs/abc?token=s3cret');
  });

  it('turns a 422 into an ApiError that keeps the partial result', async () => {
    fetchMock.mockResolvedValue(reply(422, { error: 'refused', result: { r: 1 } }));
    const err = (await api.op('d', 'rearm').catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(422);
    expect(err.message).toBe('refused');
    expect(err.result).toEqual({ r: 1 });
  });

  it('calls the unauthorized handler on 401', async () => {
    const h = vi.fn();
    onUnauthorized(h);
    fetchMock.mockResolvedValue(reply(401, { error: 'bad or missing token' }));
    await expect(api.devices()).rejects.toMatchObject({ status: 401, unauthorized: true });
    expect(h).toHaveBeenCalledOnce();
  });

  it('treats 503 from /api/health as data, not an error', async () => {
    fetchMock.mockResolvedValue(reply(503, { ok: false, devices: [] }));
    expect((await api.health()).ok).toBe(false);
  });

  it('wraps network failures and builds query strings', async () => {
    fetchMock.mockRejectedValue(new TypeError('fetch failed'));
    await expect(api.lines('d')).rejects.toMatchObject({ status: 0 });
    fetchMock.mockResolvedValue(reply(200, []));
    await api.lines('d', { boot: 3, grep: 'Fault x', limit: 10 });
    expect((fetchMock.mock.calls[1] as [string])[0]).toBe('/api/devices/d/lines?boot=3&grep=Fault+x&limit=10');
  });
});
