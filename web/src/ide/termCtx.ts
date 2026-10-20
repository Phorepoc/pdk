// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import type { InjectionKey, Ref } from 'vue';
import type { Tree, Zone } from './splitTree';

/** What the Terminal panel shares with its split nodes and groups. */
export interface TermCtx {
  tree: Tree;
  /** tab key -> the phone's tty id (0 until attached) */
  ttyOf: Record<string, number>;
  device: Readonly<Ref<string>>;
  dragKey: Ref<string>;
  add: (ttyId: number, groupId?: number) => void;
  close: (key: string) => void;
  drop: (key: string, groupId: number, zone: Zone) => void;
  gone: () => void;
  probe: () => void;
  ttys: Ref<{ id: number; cols: number; rows: number; shell: number; title: string }[]>;
}

export const TERM_CTX: InjectionKey<TermCtx> = Symbol('term');
