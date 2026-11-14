/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: AGPL-3.0-only WITH LicenseRef-Phorepoc-Build-Output-Exception-1.0 */

/* sys/syscall.h: JSC port spike stub */
#ifndef PH_SYS_SYSCALL_H
#define PH_SYS_SYSCALL_H
#define SYS_gettid 186
#ifdef __cplusplus
extern "C"
#endif
long syscall(long, ...);
#endif
