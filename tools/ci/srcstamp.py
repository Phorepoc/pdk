# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""srcstamp.py -- make a reused build tree incremental after a fresh checkout.

The emitter rebuilds an object when one of its dependencies is newer than it (mtimes, build_mmp.py _deps_newer). A
fresh `git checkout` gives every source file the current time, so a build tree restored from an earlier run (the
published build image, CI) would rebuild everything. This tool records the git blob id of every tracked file in the
source repositories next to the build, and on the next checkout gives unchanged files an old mtime (so the restored
objects are newer) and changed or new files the current time (so what they feed rebuilds).

  srcstamp.py save  <build dir> <root>...   after a build: write <build>/.srcstamp (path -> blob id)
  srcstamp.py apply <build dir> <root>...   after checkout + restore: set the mtimes from it

<root> = a directory searched for git repositories (each repo's tracked files, from `git ls-files -s`).
"""

import os
import subprocess
import sys
import time

OLD = 1_000_000_000          # 2001-09-09: older than any object a build writes


def repos(roots):
    for root in roots:
        for dirpath, dirnames, _ in os.walk(root):
            if ".git" in dirnames:
                yield dirpath
                dirnames.remove(".git")
            dirnames[:] = [d for d in dirnames if d not in ("node_modules", "build")]


def tracked(roots):
    """(absolute path, blob id) for every tracked file of every repo under the roots."""
    seen = set()
    for repo in repos(roots):
        repo = os.path.abspath(repo)
        if repo in seen:
            continue
        seen.add(repo)
        out = subprocess.run(["git", "-C", repo, "ls-files", "-s", "-z"], capture_output=True, check=True).stdout
        for rec in out.split(b"\0"):
            if not rec:
                continue
            meta, path = rec.split(b"\t", 1)
            mode, blob, _ = meta.split(b" ")
            if mode == b"160000":             # a submodule: its own repo is walked separately
                continue
            yield os.path.join(repo, path.decode("utf-8", "surrogateescape")), blob.decode()


def main(argv):
    if len(argv) < 3 or argv[0] not in ("save", "apply"):
        sys.exit(__doc__)
    cmd, build, roots = argv[0], argv[1], argv[2:]
    stamp = os.path.join(build, ".srcstamp")
    if cmd == "save":
        with open(stamp, "w", encoding="utf-8", newline="\n") as fh:
            for path, blob in tracked(roots):
                fh.write(f"{blob} {os.path.relpath(path)}\n")
        print(f"srcstamp: saved {stamp}")
        return
    old = {}
    try:
        with open(stamp, encoding="utf-8") as fh:
            for line in fh:
                blob, path = line.rstrip("\n").split(" ", 1)
                old[path] = blob
    except OSError:
        print(f"srcstamp: no {stamp}: a full build")
        return
    now = time.time()
    same = changed = 0
    for path, blob in tracked(roots):
        rel = os.path.relpath(path)
        t = OLD if old.get(rel) == blob else now
        try:
            os.utime(path, (t, t), follow_symlinks=os.utime not in os.supports_follow_symlinks)
        except OSError:
            continue
        if t == OLD:
            same += 1
        else:
            changed += 1
    print(f"srcstamp: {same} files unchanged (old mtime), {changed} changed or new (now)")


if __name__ == "__main__":
    main(sys.argv[1:])
