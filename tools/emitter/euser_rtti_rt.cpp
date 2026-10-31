// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// euser_rtti_rt.cpp -- the one libc leaf the statically linked C++ ABI runtime needs from euser.
// euser carries real typeinfo objects for its classes (compiled -frtti); their vtables come from libsupc++
// (__class_type_info & co, linked into the image by build_euser_device.py). std::type_info::operator== compares
// two typeinfo names by string when the pointers differ (ARM EABI: __GXX_MERGED_TYPEINFO_NAMES == 0), which is how
// a typeinfo in euser matches the copy inside another image. Plain strcmp semantics; no data/bss (euser is XIP
// with no writable static data).
#include <e32std.h>

extern "C" int strcmp(const char* aA, const char* aB)
	{
	while (*aA && *aA == *aB)
		{
		++aA;
		++aB;
		}
	return (int)(TUint8)*aA - (int)(TUint8)*aB;
	}
