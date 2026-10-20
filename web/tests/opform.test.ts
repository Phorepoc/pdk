// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { buildArgs, fieldsFor, validate } from '@/api/opform';
import type { OpSpec } from '@/api/types';

const spec: OpSpec = {
  name: 'x', description: '', capability: 'memory', reboots: false,
  params: [
    { name: 'addr', type: 'uint32', description: '', required: true },
    { name: 'count', type: 'int', description: '', required: false },
    { name: 'reboot', type: 'bool', description: '', required: false },
    { name: 'file', type: 'file', description: '', required: false },
    { name: 'blob', type: 'blob', description: '', required: false },
  ],
};

describe('op form generator', () => {
  it('maps param types to field kinds and marks required', () => {
    const f = fieldsFor(spec);
    expect(f.map((x) => x.kind)).toEqual(['hex', 'int', 'bool', 'text', 'text']);
    expect(f[0]!.label).toBe('addr *');
    expect(f[1]!.label).toBe('count');
  });

  it('keeps uint32 as a string, int as a number, omits empty optionals', () => {
    const a = buildArgs(spec, { addr: '0x01c20000', count: '4', reboot: null, file: '', blob: undefined });
    expect(a).toEqual({ addr: '0x01c20000', count: 4 });
    expect(typeof a.addr).toBe('string');
  });

  it('sends bool only when set, including false', () => {
    expect(buildArgs(spec, { addr: '1', reboot: false })).toEqual({ addr: '1', reboot: false });
    expect(buildArgs(spec, { addr: '1', reboot: true })).toEqual({ addr: '1', reboot: true });
  });

  it('validates required and numeric fields', () => {
    expect(validate(spec, {})).toEqual({ addr: 'required' });
    expect(validate(spec, { addr: 'zzz', count: '1.5' })).toEqual({ addr: 'hex (0x...) or decimal', count: 'integer' });
    expect(validate(spec, { addr: '0xDEADbeef', count: '8' })).toEqual({});
    expect(validate({ ...spec, params: null }, {})).toEqual({});
  });
});
