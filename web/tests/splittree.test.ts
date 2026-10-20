// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { addTab, dropTab, groupsOf, newTree, removeTab } from '@/ide/splitTree';

describe('terminal split tree', () => {
  it('splits by edge drops, joins by middle drops, collapses when groups empty', () => {
    const t = newTree();
    addTab(t, 'a'); addTab(t, 'b'); addTab(t, 'c');
    const g1 = groupsOf(t.root)[0]!.id;
    dropTab(t, 'b', g1, 'right');
    expect(t.root.kind === 'split' && t.root.dir).toBe('row');
    dropTab(t, 'c', g1, 'bottom');
    expect(groupsOf(t.root).map((g) => g.tabs)).toEqual([['a'], ['c'], ['b']]);
    const ids = groupsOf(t.root).map((g) => g.id);
    expect(new Set(ids).size).toBe(3);
    const gb = groupsOf(t.root).find((g) => g.tabs.includes('b'))!.id;
    dropTab(t, 'c', gb, 'center');
    expect(groupsOf(t.root).map((g) => g.tabs)).toEqual([['a'], ['b', 'c']]);
    dropTab(t, 'a', g1, 'left'); // its group's only tab: nothing happens
    expect(groupsOf(t.root)).toHaveLength(2);
    removeTab(t, 'b'); removeTab(t, 'c');
    expect(t.root.kind).toBe('group');
    expect(groupsOf(t.root)[0]!.tabs).toEqual(['a']);
  });
});
