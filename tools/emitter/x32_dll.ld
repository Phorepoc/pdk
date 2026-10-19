/* x32_dll.ld -- link script for a Path C (amd64 CPU, x32 ABI) DLL that becomes an E32Image.
 * The DLL sibling of x32_exe.ld (and the amd64 sibling of mmp_dll.ld): same elf.extract
 * contract -- ".text = code + rodata", so absolute R_X86_64_32 words in .rodata become
 * ordinary TEXT relocs -- with two DLL deltas:
 *   - .text.entry is placed FIRST, so _E32Dll (port/x64user/xu_dll.cpp) owns code offset 0 =
 *     the image entry point, with its 16-byte header (UID3 at +12) where the loader wants it.
 *   - .init_array is bracketed and KEEPed: _E32Dll_Body walks it on process attach, which is
 *     how a separately-loaded module gets its static constructors run (the static uexe link
 *     had the EXE's own _E32Startup do this).
 * Link with  ld -m elf32_x86_64 -q  (--emit-relocs). */
ENTRY(_E32Dll)
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
