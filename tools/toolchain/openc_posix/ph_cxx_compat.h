/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

// ph_cxx_compat.h -- force-included into the libstdc++ build (and C++17 clients): declarations Open C / P.I.P.S. lacks or
// renames that libstdc++'s headers name. Declarations only: nothing links to them unless the program uses the feature.
#ifndef __ASSEMBLER__   // (ICU data stubs are .S files run through the same cc line)
#ifndef PH_CXX_COMPAT_H
#define PH_CXX_COMPAT_H
#ifdef __cplusplus
#ifndef __STDC_LIMIT_MACROS
#define __STDC_LIMIT_MACROS 1
#endif
#ifndef __STDC_CONSTANT_MACROS
#define __STDC_CONSTANT_MACROS 1
#endif
#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
// Open C's stdio.h renames these with macros (vfscanf -> __vfscanf); libstdc++'s <cstdio> #undef's them and `using ::vfscanf`.
#undef vfscanf
#undef vscanf
#undef vsscanf
extern "C" {
int vfscanf(FILE*, const char*, va_list) __asm__("__vfscanf");
int vscanf(const char*, va_list) __asm__("__vscanf");
int vsscanf(const char*, const char*, va_list) __asm__("__vsscanf");
int pthread_cancel(pthread_t);
}
typedef struct { long long __ll; long double __ld; } max_align_t;
namespace std { using ::max_align_t; }
#endif
#endif
// ---- JSC/WebKit spike additions: POSIX/glibc names Open C lacks (declarations only; each is a porting item, see browser-plan.md P0 results)
#ifdef __cplusplus
extern "C" {
#else
#include <stddef.h>
#endif
void* aligned_alloc(size_t, size_t);
int posix_memalign(void**, size_t, size_t);
void* memmem(const void*, size_t, const void*, size_t);
char* mkdtemp(char*);
int fallocate(int, int, long long, long long);
int dladdr(const void*, void*);
int madvise(void*, size_t, int);
int pthread_getattr_np(unsigned long, void*);
int getrlimit(int, void*);
int getrusage(int, void*);
int mkostemp(char*, int);
unsigned long long strtoull(const char*, char**, int);
long long llabs(long long);
int pthread_attr_getstack(const void*, void**, size_t*);
#ifdef __cplusplus
}
#endif
#ifndef MAP_ANON
#define MAP_ANON 0x20
#endif
#ifndef CLOCK_BOOTTIME
#define CLOCK_BOOTTIME 7
#endif
// ---- C99 <math.h>/<ctype.h> names Open C's libm lacks (found by cmath_probe.sh); declarations only -> link stubs until libm grows them
#ifdef __cplusplus
extern "C" {
#endif
long double tgammal(long double); long double roundl(long double); long double rintl(long double); long double nearbyintl(long double);
long double log2l(long double); long double log1pl(long double); long double lgammal(long double); long double expm1l(long double);
long double exp2l(long double); long double erfl(long double); long double erfcl(long double); long double cbrtl(long double);
long double atanhl(long double); long double asinhl(long double); long double acoshl(long double); long double logbl(long double);
long double scalbnl(long double, int); long double remainderl(long double, long double); long double nextafterl(long double, long double);
long double nexttowardl(long double, long double); double nexttoward(double, long double); long double fminl(long double, long double);
long double hypotl(long double, long double); long double fmal(long double, long double, long double); long double remquol(long double, long double, int*);
long lroundl(long double); long lrintl(long double); long long llroundl(long double); long long llrintl(long double);
long double nanl(const char*); float nanf(const char*); double nan(const char*);
double tgamma(double); double log2(double); float tgammaf(float); float log2f(float);
int isblank(int);
#ifdef __cplusplus
}
#endif
#ifndef MADV_NORMAL
#define MADV_NORMAL 0
#define MADV_DONTNEED 4
#define MADV_DONTDUMP 16
#define MADV_DODUMP 17
#endif
#ifndef MAP_NORESERVE
#define MAP_NORESERVE 0x4000
#endif
#ifndef O_CLOEXEC
#define O_CLOEXEC 0x80000
#endif
#ifndef SCHED_OTHER
#define SCHED_OTHER 0
#define SCHED_IDLE 5
#endif
#ifndef __cplusplus
typedef int wchar_t;   /* C TUs: the wrapper passes -D__wchar_t_defined (Open C's typedef is skipped) */
#endif
// ---- round 3 (WTF OS seams): constants/decls Open C lacks
#ifndef CLOCK_MONOTONIC_COARSE
#define CLOCK_MONOTONIC_COARSE 6
#endif
#ifndef CLOCK_THREAD_CPUTIME_ID
#define CLOCK_THREAD_CPUTIME_ID 3
#endif
#ifndef MADV_WILLNEED
#define MADV_WILLNEED 3
#endif
#ifndef SCHED_BATCH
#define SCHED_BATCH 3
#endif
#ifndef PR_SET_NAME
#define PR_SET_NAME 15
#endif
#ifdef __cplusplus
extern "C" {
#endif
int pthread_attr_setstack(void*, void*, size_t);
int sched_setscheduler(int, int, const void*);
#ifdef __cplusplus
}
#endif
// ---- P1 (ICU / WTF): PATH_MAX is not defined by Open C's <limits.h>
#include <limits.h>
#ifndef PATH_MAX
#define PATH_MAX 4096
#endif

#ifndef __ASSEMBLER__
// ---- P1 (WTF/JSC on the pure Open C line): POSIX names Open C hides behind SYMBIAN_OE_POSIX_SIGNALS or lacks (OURS: libphcompat.a)
#include <sys/types.h>
#include <pthread.h>
#include <signal.h>
#ifdef __cplusplus
extern "C" {
#endif
int sigsuspend(const sigset_t*); int pthread_sigmask(int, const sigset_t*, sigset_t*); int pthread_kill(pthread_t, int); ssize_t pread(int, void*, size_t, off_t);
int sigfillset(sigset_t*); int sigaddset(sigset_t*, int); int sigdelset(sigset_t*, int); int sigismember(const sigset_t*, int);
#ifdef __cplusplus
}
#endif
#ifndef MAP_FILE
#define MAP_FILE 0
#endif
#ifndef _SC_PHYS_PAGES
#define _SC_PHYS_PAGES 200
#endif
#endif
#endif /* !__ASSEMBLER__ */
