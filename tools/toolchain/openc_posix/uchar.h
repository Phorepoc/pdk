/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* uchar.h -- OURS: Open C has none. simdutf's C API (and others) test __has_include(<uchar.h>) and otherwise #define char16_t uint16_t,
   which breaks the C++ overloads. C++ has char16_t/char32_t built in; C gets typedefs. */
#ifndef PH_UCHAR_H
#define PH_UCHAR_H
#include <stdint.h>
#ifndef __cplusplus
typedef uint_least16_t char16_t;
typedef uint_least32_t char32_t;
#endif
#endif
