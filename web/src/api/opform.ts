// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Turns an op spec into form fields and form values into the JSON args the server wants.
import type { OpParam, OpSpec } from './types';

export type FieldKind = 'text' | 'hex' | 'int' | 'bool';

export interface Field {
  name: string;
  kind: FieldKind;
  required: boolean;
  description: string;
  type: string;
  label: string;
}

export type FormValues = Record<string, string | number | boolean | null | undefined>;

const HEX_OR_DEC = /^(0x[0-9a-f]+|[0-9]+)$/i;

export function fieldKind(type: string): FieldKind {
  switch (type) {
    case 'uint32': return 'hex';
    case 'int': return 'int';
    case 'bool': return 'bool';
    default: return 'text'; // string, blob (sha), file (server path)
  }
}

export function fieldsFor(spec: OpSpec): Field[] {
  return (spec.params ?? []).map((p: OpParam) => ({
    name: p.name,
    kind: fieldKind(p.type),
    required: p.required,
    description: p.description,
    type: p.type,
    label: p.required ? `${p.name} *` : p.name,
  }));
}

/** Returns an error message per invalid field (empty object = valid). */
export function validate(spec: OpSpec, values: FormValues): Record<string, string> {
  const errs: Record<string, string> = {};
  for (const f of fieldsFor(spec)) {
    const v = values[f.name];
    const empty = v === undefined || v === null || v === '';
    if (empty) {
      if (f.required) errs[f.name] = 'required';
      continue;
    }
    if (f.kind === 'hex' && !HEX_OR_DEC.test(String(v).trim())) errs[f.name] = 'hex (0x...) or decimal';
    if (f.kind === 'int' && !Number.isInteger(Number(v))) errs[f.name] = 'integer';
  }
  return errs;
}

/** uint32 stays a string (hex text such as 0x01c20000 passes through untouched); int becomes a number;
 *  bool is sent only when set (null = leave the server default); empty optional fields are omitted. */
export function buildArgs(spec: OpSpec, values: FormValues): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fieldsFor(spec)) {
    const v = values[f.name];
    if (v === undefined || v === null || v === '') continue;
    if (f.kind === 'int') out[f.name] = Number(v);
    else if (f.kind === 'bool') out[f.name] = Boolean(v);
    else out[f.name] = String(v).trim();
  }
  return out;
}
