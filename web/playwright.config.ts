// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// End-to-end checks against a running PhoneLab with a phone attached (e2e/); unit tests stay in vitest (tests/).
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: 'e2e',
  workers: 1,
  reporter: 'list',
  use: { channel: process.env.PW_CHANNEL ?? 'msedge', headless: true },
});
