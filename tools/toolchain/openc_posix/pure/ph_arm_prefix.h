/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

// ph_arm_prefix.h -- force-included first on the ARM "pure Open C" line (mk_pure_wrappers.sh ARCH=arm, the PinePhone browser).
// The ARM line keeps -D_STDARG_H -D_STDARG_E_H -D__e32_va_list=va_list (as build_mmp.py gives a stdapis client): Open C's
// char* va_list is skipped, but those guards also skip GCC's own <stdarg.h>, so va_list and the va_* macros come from here --
// the compiler's AAPCS va_list, the type Open C's ARM libc was built with. IMPORT_C/EXPORT_C as overlay/pure/e32def.h.
#if !defined(PH_ARM_PREFIX_H) && !defined(__ASSEMBLER__)   // (ICU data .S files run through the same line)
#define PH_ARM_PREFIX_H
typedef __builtin_va_list va_list;
#define va_start(v, l) __builtin_va_start(v, l)
#define va_end(v) __builtin_va_end(v)
#define va_arg(v, l) __builtin_va_arg(v, l)
#define va_copy(d, s) __builtin_va_copy(d, s)
#define __va_copy(d, s) __builtin_va_copy(d, s)
#ifndef __GNUC_VA_LIST
#define __GNUC_VA_LIST
typedef __builtin_va_list __gnuc_va_list;
#endif
#include "e32def.h"
#endif
