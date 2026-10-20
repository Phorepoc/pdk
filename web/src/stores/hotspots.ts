// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { ref } from 'vue';

// Hotspots panel state, kept outside the component: the panel lives in a side dialog that unmounts when closed,
// and a sampling run (or its result) must survive closing and reopening it.
export interface HotRow { thread: string; hits: number; pct: number }
export interface HotFunc { function: string; image: string; thread: string; hits: number; pct: number; callers?: HotRow[] }
export interface HotResult {
  seconds: number; samples: number; idle_samples: number; lost: number; symbols: string;
  functions: HotFunc[]; images: HotRow[]; threads: HotRow[];
}

export const hot = {
  secs: ref(10),
  symbols: ref('.pinephone-build/symbian.syms'),
  running: ref(false),
  busy: ref(false),
  since: ref(''),
  error: ref(''),
  res: ref<HotResult | null>(null),
};
