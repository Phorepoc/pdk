/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* poll.h -- OURS (Phorepoc): Open C has select() but no poll(). POSIX declarations; poll() is libphcompat's (compat/phpoll.c,
 * built on select()). Used by OpenSSL's BIO socket code. */
#ifndef PH_POLL_H
#define PH_POLL_H
#ifdef __cplusplus
extern "C" {
#endif

struct pollfd {
    int fd;
    short events;
    short revents;
};
typedef unsigned int nfds_t;

#define POLLIN     0x0001
#define POLLPRI    0x0002
#define POLLOUT    0x0004
#define POLLERR    0x0008
#define POLLHUP    0x0010
#define POLLNVAL   0x0020
#define POLLRDNORM 0x0040
#define POLLRDBAND 0x0080
#define POLLWRNORM POLLOUT
#define POLLWRBAND 0x0100

int poll(struct pollfd *fds, nfds_t nfds, int timeout);

#ifdef __cplusplus
}
#endif
#endif
