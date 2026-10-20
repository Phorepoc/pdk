/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

// e32def.h -- OURS stand-in for engine code (WTF/JSC/ICU) compiled on the "pure Open C" line (browser-plan.md P1).
// Open C's stdapis headers #include <e32def.h> only for IMPORT_C/EXPORT_C. The real e32def.h defines ASSERT / __ASSERT_DEBUG /
// TInt... which collide with WTF's ASSERT; this stub is found first (-I overlay/pure) and defines nothing but the export markers.
#ifndef PH_PURE_E32DEF_H
#define PH_PURE_E32DEF_H
#ifndef IMPORT_C
#define IMPORT_C
#endif
#ifndef EXPORT_C
#define EXPORT_C
#endif
#endif
