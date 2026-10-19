/* Track U P1b — link script for the mbedTLS SSL.DLL (and its headless self-tests).
 * Single .text (code + rodata + C++ vtables/rel.ro folded in) at 0x8000, then
 * .data / .bss. ENTRY is the gc-sections root; the real export set is pinned with
 * -Wl,-u on the link line so --gc-sections keeps them.
 *
 * C++ notes: vtables/typeinfo land in .data.rel.ro (they hold absolute code
 * addresses needing runtime relocation). We fold them into .text — they are
 * read-only after the loader applies the ABS32 fixups, and keeping them in the
 * code image means extract() sees their relocs as code->code (RELOC_TEXT).
 * .init_array/.fini_array are expected EMPTY (no file-scope ctors; the one static
 * is a guarded function-local); they are placed so the linker never orphans them
 * to a high address — a non-empty .init_array would need a load-time ctor call
 * the loader does not make, so the builder asserts it stayed empty. */
ENTRY(selftest_crypto)
SECTIONS {
  . = 0x8000;
  .text : {
    *(.text) *(.text.*)
    *(.rodata) *(.rodata.*)
    *(.data.rel.ro) *(.data.rel.ro.*) *(.gnu.linkonce.d.rel.ro.*)
    *(.glue_7) *(.glue_7t) *(.gnu.linkonce.t.*)
  }
  . = ALIGN(8);
  .init_array : { PROVIDE(__init_array_start = .); KEEP(*(SORT(.init_array.*))) KEEP(*(.init_array)) PROVIDE(__init_array_end = .); }
  .fini_array : { KEEP(*(.fini_array)) KEEP(*(SORT(.fini_array.*))) }
  . = ALIGN(8);
  .data : { *(.data) *(.data.*) *(.got) *(.got.plt) }
  .bss  : { *(.bss) *(.bss.*) *(COMMON) }
  /DISCARD/ : { *(.ARM.attributes) *(.comment) *(.note*) *(.ARM.exidx*) *(.ARM.extab*) *(.eh_frame*) }
}
