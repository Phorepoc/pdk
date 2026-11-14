// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
// phedbg_caps.h -- runtime capability helpers.
#ifndef PHEDBG_CAPS_H
#define PHEDBG_CAPS_H
#include <e32base.h>
inline TBool PheDbgHasCap(TCapability aCap) { return RProcess().HasCapability(aCap); }
#define PHEDBG_CAP_SWEVENT  PheDbgHasCap(ECapabilitySwEvent)
#define PHEDBG_CAP_ALLFILES PheDbgHasCap(ECapabilityAllFiles)
#endif
