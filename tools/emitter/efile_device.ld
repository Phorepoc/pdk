/* Copyright The Phorepoc Authors
   SPDX-License-Identifier: Apache-2.0 */

/* efile_device.ld -- efile.ld for a `device` EXE: the unwind tables kept, as in mmp_dll_device.ld (see there). */
ENTRY(_E32Startup)
SECTIONS {
  . = 0x8000;
  .text : {
    __symbian_ro_base = .;
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
    LONG(0)
  }
  . = ALIGN(8);
  .data : { *(.data) *(.data.*) }
  .bss  : { *(.bss) *(.bss.*) *(COMMON) }
  /DISCARD/ : { *(.ARM.attributes) *(.comment) *(.note*) }
}
