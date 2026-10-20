// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

/* mmp_dll_device.ld -- mmp_dll.ld for a `device` DLL (the REAL phone; C++ exceptions on). The phone's unwinder
 * (drtaeabi, kernel/eka/compsupp/symaehabi) finds a RAM-loaded image's tables only through the E32 header's
 * iExceptionDescriptor -> TExceptionDescriptor {exidx base, exidx limit, RO base (+1 = EHABI v2), RO limit}, all of it
 * inside the code section (elf2e32 does the same from Symbian$$CPP$$Exception$$Descriptor). So the tables are KEPT:
 * .ARM.extab inside .text, .ARM.exidx in its own output section right after it (ld sorts it and fixes its coverage
 * only there) -- elf.extract folds it into the code bytes -- and toolchain/dll_rt/device_excdesc.s holds the
 * descriptor. */
ENTRY(_E32Dll)
SECTIONS {
  . = 0x8000;
  .text : {
    __symbian_ro_base = .;
    *dll_entry.s.o(.text)
    *(.text) *(.text.*) *(.rodata) *(.rodata.*)
    *(.ARM.extab) *(.ARM.extab.*)
    . = ALIGN(4);
    __init_array_start = .;
    KEEP(*(SORT_BY_INIT_PRIORITY(.init_array.*)))
    KEEP(*(.init_array))
    __init_array_end = .;
    LONG(0)
  }
  . = ALIGN(4);
  .ARM.exidx : {
    __exidx_start = .;
    *(.ARM.exidx) *(.ARM.exidx.*)
    __exidx_end = .;
    LONG(0)   /* keeps __exidx_end (= the RO limit) strictly inside the code range */
  }
  . = ALIGN(8);
  .data : { *(.data) *(.data.*) }
  .bss  : { *(.bss) *(.bss.*) *(COMMON) }
  /DISCARD/ : { *(.ARM.attributes) *(.comment) *(.note*) }
}
