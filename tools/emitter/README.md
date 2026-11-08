# pdk/tools/emitter

The x32 / ARM image emitter: ELF to E32Image (`e32image.py`, `build_mmp_x32.py`, `elf.py` ...).

## x32 import table: the PC-relative flag

x32 images use ELF-derived imports: each import-block entry is the code offset of a 32-bit word holding
`ordinal | (adjustment << 16)`; the loader replaces it with `export + adjustment`.

For RAM-loaded (`sis=True`) images `build_mmp_x32.py` also lists `%rip`-relative references to
imported statics here, flagged by **bit 15 of the ordinal field** with a signed 16-bit addend in the upper half. The x32 board
loader (core `os/toolchain/x32/f32/sf_lepoc.cpp`, `svElfDerivedFixupImportAddresses`) writes
`export + addend - address of the word`. ROM images never carry the flag (the ROM prelinker aims those sites from the `.pcrel`
sidecar), and ARM images do not go through this code. Full description: the e32image spec,
section 7b (`docs/formats/e32image-spec.md` in the research repo).
