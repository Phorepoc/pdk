// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { config } from '@vue/test-utils';
import * as Q from 'quasar';

// Register every Quasar component globally (the app build auto-imports them per SFC instead).
const components: Record<string, unknown> = {};
for (const [k, v] of Object.entries(Q)) if (k.startsWith('Q') && typeof v === 'object') components[k] = v;
config.global.plugins = [[Q.Quasar, {
  components,
  directives: { Ripple: Q.Ripple, ClosePopup: Q.ClosePopup },
  plugins: { Notify: Q.Notify, Dialog: Q.Dialog },
}]];

// jsdom lacks element scrolling; QVirtualScroll calls it
Element.prototype.scrollTo = (() => undefined) as typeof Element.prototype.scrollTo;
window.scrollTo = (() => undefined) as typeof window.scrollTo;
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
