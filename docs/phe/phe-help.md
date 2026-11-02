# phe help

Show the commands, a command's manual page, or a topic.

## Synopsis

```
phe help [-markdown <directory>] [<command>|<topic>]
```

## Description

Without operands, the list of commands. With a command ("phe help image rom"), its manual page: synopsis, description, options and examples. With a topic ("phe help environment"), that topic. -markdown writes every manual page as Markdown into a directory (the pdk's docs/phe/).

## Options

- `-markdown <directory>`: Write every manual page as Markdown into this directory.

## Examples

How to build the ROM:

```sh
phe help image rom
```

Regenerate the documentation:

```sh
phe help -markdown docs/phe
```

