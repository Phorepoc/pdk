// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { defineRouter } from '#q-app';
import { createMemoryHistory, createRouter, createWebHistory } from 'vue-router';
import routes from './routes';

export default defineRouter(() => {
  const createHistory = import.meta.env.QUASAR_SERVER ? createMemoryHistory : createWebHistory;
  const router = createRouter({
    scrollBehavior: () => ({ left: 0, top: 0 }),
    routes,
    history: createHistory(import.meta.env.QUASAR_VUE_ROUTER_BASE),
  });
  return router;
});
