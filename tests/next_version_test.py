#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
"""next_version_test.py -- tools/ci/next_version.sh: a changed board.lock pin is a patch release, whatever the subject."""
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "..", "tools", "ci", "next_version.sh")


def git(d, *a):
    return subprocess.run(["git", "-C", d, "-c", "user.name=t", "-c", "user.email=t@t", *a], check=True,
                          capture_output=True, text=True).stdout


def commit(d, subject, files):
    for name, text in files.items():
        with open(os.path.join(d, name), "w", newline="\n") as f:
            f.write(text)
    git(d, "add", "-A")
    git(d, "commit", "-q", "-m", subject)


def nv(d):
    return subprocess.run(["sh", SCRIPT, d], check=True, capture_output=True, text=True).stdout.strip()


LOCK = "# pins\ncore %s\ntls %s\n"
cases = []
with tempfile.TemporaryDirectory() as d:
    git(d, "init", "-q")
    commit(d, "feat: first", {"board.lock": LOCK % ("a" * 40, "b" * 40), "x": "1"})
    git(d, "tag", "v1.2.3")
    cases.append(("no commits since the tag", nv(d), ""))
    commit(d, "docs: words", {"x": "2"})
    commit(d, "test: more", {"board.lock": LOCK % ("a" * 40, "b" * 40) + "\n"})   # blank line only
    commit(d, "chore: comment", {"board.lock": "# other comment\ncore %s\ntls %s\n" % ("a" * 40, "b" * 40)})
    cases.append(("docs/test/comment-only lock edits", nv(d), ""))
    commit(d, "chore(lock): core bump", {"board.lock": LOCK % ("c" * 40, "b" * 40)})
    cases.append(("a changed pin under a chore subject", nv(d), "v1.2.4"))
    commit(d, "fix: something", {"x": "3"})
    cases.append(("fix + pin still a patch", nv(d), "v1.2.4"))
    commit(d, "feat: new", {"x": "4"})
    cases.append(("feat wins", nv(d), "v1.3.0"))
    git(d, "tag", "v1.3.0")
    commit(d, "not conventional", {"board.lock": LOCK % ("c" * 40, "d" * 40)})
    cases.append(("a pin under a free-form subject", nv(d), "v1.3.1"))

bad = [(n, got, want) for n, got, want in cases if got != want]
for n, got, want in cases:
    print(("FAIL " if got != want else "PASS ") + n + ": " + repr(got) + (" want " + repr(want) if got != want else ""))
sys.exit(1 if bad else 0)
