/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: AGPL-3.0-only WITH LicenseRef-Phorepoc-Build-Output-Exception-1.0 */

/* sys/prctl.h: JSC port spike stub */
#ifndef PH_SYS_PRCTL_H
#define PH_SYS_PRCTL_H
#define PR_SET_VMA 0x53564d41
#define PR_SET_VMA_ANON_NAME 0
#ifdef __cplusplus
extern "C"
#endif
int prctl(int, ...);
#endif
