# ph help

Show the commands, a command's manual page, or a topic.

## Synopsis

```
ph help [-markdown <directory>] [<command>|<topic>]
```

## Description

Without operands, the list of commands. With a command ("ph help image rom"), its manual page: synopsis, description, options and examples. With a topic ("ph help environment"), that topic. -markdown writes every manual page as Markdown into a directory (the pdk's docs/ph/).

## Options

- `-markdown <directory>`: Write every manual page as Markdown into this directory.

## Examples

How to build the ROM:

```sh
ph help image rom
```

Regenerate the documentation:

```sh
ph help -markdown docs/ph
```

