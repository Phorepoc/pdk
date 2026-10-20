/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* netinet/tcp.h -- OURS (Phorepoc): Open C has no <netinet/tcp.h>; its TCP socket options (TCP_NODELAY = KSoTcpNoDelay,
 * TCP_MAXSEG) live in <netinet/in.h>. Ports that include the BSD header (OpenSSL's sockets.h) get them from there. */
#ifndef PH_NETINET_TCP_H
#define PH_NETINET_TCP_H
#include <netinet/in.h>
#endif
