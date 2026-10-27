/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

// ph_x32_prefix.h -- force-included first on the x32 "pure Open C" line (mk_pure_wrappers.sh ARCH=x32: FFmpeg, OpenH264 ...).
// Open C's <stdarg.h> / <stdarg_e.h> make va_list a char* walked over the stack: right for its old ARM ABI, wrong on x32,
// where libc.dll (built by build_mmp_x32 with the compiler's own <stdarg.h>) reads the System V va_list structure. A library
// built with the char* one hands vsnprintf a pointer it takes for that structure (FFmpeg's av_log -> Open C __vfprintf faulted
// on a NULL overflow_arg_area). So, as on ARM (ph_arm_prefix.h): the compiler's va_list, and Open C's two headers skipped.
#if !defined(PH_X32_PREFIX_H) && !defined(__ASSEMBLER__)
#define PH_X32_PREFIX_H
#define _STDARG_H
#define _STDARG_E_H
#define _ANSI_STDARG_H_
#define __STDARG_H
typedef __builtin_va_list va_list;
#define va_start(v, l) __builtin_va_start(v, l)
#define va_end(v) __builtin_va_end(v)
#define va_arg(v, l) __builtin_va_arg(v, l)
#define va_copy(d, s) __builtin_va_copy(d, s)
#define __va_copy(d, s) __builtin_va_copy(d, s)
#define _VA_COPY_DEFINED    // Open C's <stdio.h> would make va_copy an assignment (x32's va_list is an array)
#define __e32_va_list va_list
#ifndef __GNUC_VA_LIST
#define __GNUC_VA_LIST
typedef __builtin_va_list __gnuc_va_list;
#endif
#endif
