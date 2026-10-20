/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* sqlite_compat.h -- OURS (Phorepoc), force-included into the SQLite amalgamation (build_webdeps.sh). SQLite's unix VFS defines
 * HAVE_FCHOWN for every non-VxWorks system; Open C has no fchown (Symbian files have no owner): a successful no-op is the truth here. */
#ifndef PH_SQLITE_COMPAT_H
#define PH_SQLITE_COMPAT_H
#include <sys/types.h>
static inline int fchown(int fd, uid_t owner, gid_t group) { (void)fd; (void)owner; (void)group; return 0; }
#endif
