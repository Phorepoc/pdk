// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// euser_x32_rt.cpp -- the two leaves the statically linked x32 C++ ABI runtime needs from euser.
// The typeinfo classes of the distro libsupc++.a (linked by build_euser_x32.py) have deleting
// destructors, which end in the sized operator delete (C++14); and the library is built with
// -fstack-protector-strong, so its members call __stack_chk_fail. A smashed stack in euser is a
// panic, never a silent return: the platform's "this cannot happen" (User::Invariant).
// No writable data (euser is XIP).
#include <e32std.h>

void operator delete(TAny* aPtr, TUint) __NO_THROW
	{
	operator delete(aPtr);
	}

extern "C" void __stack_chk_fail()
	{
	for (;;)
		User::Invariant();
	}
