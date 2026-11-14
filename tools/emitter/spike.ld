/* Copyright The Phorepoc Authors
   SPDX-License-Identifier: AGPL-3.0-only WITH LicenseRef-Phorepoc-Build-Output-Exception-1.0 */

ENTRY(spike_value)
SECTIONS {
  . = 0x8000;
  .text : { *(.text) *(.text.*) *(.rodata) *(.rodata.*) *(.rodata) }
  . = ALIGN(4);
  .data : { *(.data) *(.data.*) }
  .bss  : { *(.bss) *(.bss.*) *(COMMON) }
  /DISCARD/ : { *(.ARM.attributes) *(.comment) *(.note*) }
}
