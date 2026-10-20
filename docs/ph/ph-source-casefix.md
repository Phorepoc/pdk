# ph source casefix

Link the header spellings a case-sensitive file system misses.

## Synopsis

```
ph source casefix [-a] [-b <board>] [<package>...]
```

## Description

The 2010 source was written for Windows: #include <AknUtils.h> finds aknutils.h. For every include spelling that names a header under another letter case, casefix puts a link with that spelling into <build>/casefix, which the compilers search last. Where several headers share a name, the exported one in the order the components' SYSTEMINCLUDE paths search wins. No source file is changed.

## Options

- `-a`: Scan every package of both source trees.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Refresh the links after a full export:

```sh
ph source casefix -a
```

## Formerly

Still accepted for one release:

- `ph casefix`
