/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* sys/syscall.h: JSC port spike stub */
#ifndef PH_SYS_SYSCALL_H
#define PH_SYS_SYSCALL_H
#define SYS_gettid 186
#ifdef __cplusplus
extern "C"
#endif
long syscall(long, ...);
#endif
