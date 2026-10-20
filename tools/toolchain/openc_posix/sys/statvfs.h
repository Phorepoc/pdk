/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* sys/statvfs.h: JSC port spike stub */
#ifndef PH_SYS_STATVFS_H
#define PH_SYS_STATVFS_H
struct statvfs { unsigned long f_bsize, f_frsize, f_blocks, f_bfree, f_bavail, f_files, f_ffree, f_favail, f_fsid, f_flag, f_namemax; };
#ifdef __cplusplus
extern "C"
#endif
int statvfs(const char*, struct statvfs*);
#endif
