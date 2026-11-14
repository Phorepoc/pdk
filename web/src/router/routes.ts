// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  { path: '/', component: () => import('@/ide/IdeLayout.vue') },
  { path: '/:catchAll(.*)*', redirect: '/' },
];

export default routes;
