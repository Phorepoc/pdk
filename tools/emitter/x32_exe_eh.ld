// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

/* x32_exe.ld -- link script for a Path C (amd64 CPU, x32 ABI) EXE that becomes an E32Image.
 * The amd64 sibling of efile.ld, same contract with elf.extract: ".text = code + rodata" (so
 * .rodata and the static-ctor table live INSIDE the .text output section and their absolute
 * R_X86_64_32 words become ordinary TEXT relocs), then an 8-aligned writable segment.
 * Link with  ld -m elf32_x86_64 -q  (--emit-relocs). x32 objects carry RELA relocations; the
 * section bytes are fully relocated by the static link, exactly as the E32 loader expects. */
ENTRY(_E32Startup)
SECTIONS {
  . = 0x8000;
  .text : {
    *(.text.entry)
    *(.text) *(.text.*) *(.rodata) *(.rodata.*) *(.data.rel.ro) *(.data.rel.ro.*)
    . = ALIGN(8);
    __init_array_start = .;
    KEEP(*(SORT_BY_INIT_PRIORITY(.init_array.*)))
    KEEP(*(.init_array))
    KEEP(*(.ctors))
    __init_array_end = .;
    /* C++ exceptions (component option eh=True): .eh_frame + LSDAs live in the code segment (absolute pointers = ordinary TEXT relocs) */
    . = ALIGN(4);
    __eh_frame_start = .;
    KEEP(*(.ph_eh_frame))   /* .eh_frame renamed by objcopy (build_cxxtest.sh): ld rewrites .eh_frame (drops/merges entries) but leaves the --emit-relocs entries behind */
    LONG(0)
    *(.gcc_except_table) *(.gcc_except_table.*)
    LONG(0)   /* keeps __init_array_end strictly inside the code range (see efile.ld) */
  }
  /* 64-align the writable segment. The XIP prelink shifts every data/bss word by
     (run_base - this VMA), so an object keeps its alignment only if that shift is a
     multiple of it. Run bases are page- or 0x60000000-aligned, so anchoring .data at a
     64 boundary makes the shift a multiple of everything g++ asks for -- .bss alone
     comes out 32-aligned, and a misaligned `movdqa`/`movaps` on x86-64 is a #GP, not a
     slow access. (ARM's efile.ld needs only 8; see its note.) */
  . = ALIGN(64);
  .data : { *(.data) *(.data.*) }
  .bss  : { *(.bss) *(.bss.*) *(COMMON) }
  /DISCARD/ : { *(.comment) *(.note*) *(.eh_frame*) }
}
