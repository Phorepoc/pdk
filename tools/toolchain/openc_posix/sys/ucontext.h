/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* sys/ucontext.h: JSC port spike stub -- Open C has no ucontext (signal-based thread suspension / register capture is a porting item) */
#ifndef PH_SYS_UCONTEXT_H
#define PH_SYS_UCONTEXT_H
enum { REG_R8 = 0, REG_R9, REG_R10, REG_R11, REG_R12, REG_R13, REG_R14, REG_R15, REG_RDI, REG_RSI, REG_RBP, REG_RBX, REG_RDX, REG_RAX, REG_RCX, REG_RSP, REG_RIP, REG_EFL, NGREG };
typedef struct { long long gregs[NGREG]; } mcontext_t;
typedef struct ucontext { struct ucontext* uc_link; mcontext_t uc_mcontext; } ucontext_t;
#endif
