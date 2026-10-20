# ph source export

Build the include tree from the packages' exports.

## Synopsis

```
ph source export [-a] [-b <board>] [<package>...]
```

## Description

Does what abld export did: preprocesses each package's bld.inf files and copies their PRJ_EXPORTS headers into <build>/epoc32/include, the tree every component compiles against.

Without operands, the operating-system and middleware packages the core build needs; -a exports every package of both source trees, in two passes around casefix (a bld.inf that names a header under another letter case only preprocesses once casefix has linked that spelling). Where two packages export the same header, the Symbian^3 RCL_3 line wins.

## Options

- `-a`: Every package of both source trees (runs casefix itself).
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

The include tree a clean build needs:

```sh
ph source export -a
```

Re-export one package after changing its headers:

```sh
ph source export ../symbian-rcl3/oss.FCL.sf.mw.classicui
```

## Formerly

Still accepted for one release:

- `ph export`
