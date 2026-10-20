/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* sys/sysinfo.h: JSC port spike stub (RAMSize/AvailableMemory -> HAL on Symbian) */
#ifndef PH_SYS_SYSINFO_H
#define PH_SYS_SYSINFO_H
struct sysinfo { long uptime; unsigned long loads[3]; unsigned long totalram, freeram, sharedram, bufferram, totalswap, freeswap; unsigned short procs; unsigned long totalhigh, freehigh; unsigned int mem_unit; };
#ifdef __cplusplus
extern "C"
#endif
int sysinfo(struct sysinfo*);
#endif
