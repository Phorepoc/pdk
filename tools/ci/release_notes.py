#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
"""release_notes.py -- the "What changed" part of a release's notes.

    release_notes.py --repo <owner/name> [--version <vX.Y.Z>] --built-from <built-from.txt> [--extra notes.md] [--out notes.md]

A release is built from the commits listed in built-from.txt (one "<repository> <commit>" per line, every repository
the image or package is made of). This finds the newest earlier release of the same repository, reads its
built-from.txt, and lists, for every repository whose commit moved, the commits in between -- grouped the way
next_version.sh reads them (Conventional Commits: breaking changes, "feat", "fix" / "perf", then the rest). A
repository that is new in this release, or that went back, is said so. The commit list comes from the GitHub compare
API (gh api), so nothing needs to be cloned; the repositories live under the same owner as --repo.

With no earlier release (or none that has a built-from.txt) the notes say that this is the first one. The commit
table follows, as before.
"""
import argparse
import os
import json
import re
import subprocess
import sys

TYPES = [  # (heading, types) in the order they are shown
    ("Features", {"feat"}),
    ("Fixes", {"fix", "perf"}),
    ("Other changes", None),  # everything else (docs, chore, ci, build, refactor, test, style, revert, untyped)
]
SUBJECT = re.compile(r"^(?P<type>[a-zA-Z]+)(?:\((?P<scope>[^)]*)\))?(?P<bang>!)?:\s*(?P<text>.*)$")
COMPARE_MAX = 250  # the compare API returns at most 250 commits; past that the list says how many were left out


def gh(*args):
    r = subprocess.run(["gh", *args], capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError("gh %s: %s" % (" ".join(args), r.stderr.strip()[:300]))
    return r.stdout


def read_built_from(text):
    pins = {}
    for line in text.splitlines():
        parts = line.split()
        if len(parts) == 2 and re.fullmatch(r"[0-9a-f]{40}", parts[1]):
            pins[parts[0]] = parts[1]
    return pins


def previous_release(repo, version):
    """(tag, pins) of the newest published release before this one that has a built-from.txt; (None, {}) if none."""
    releases = json.loads(gh("api", "repos/%s/releases?per_page=30" % repo))
    for rel in releases:  # newest first
        if rel.get("draft") or rel.get("tag_name") == version:
            continue
        for asset in rel.get("assets", []):
            if asset.get("name") == "built-from.txt":
                text = gh("api", "-H", "Accept: application/octet-stream", asset["url"])
                return rel["tag_name"], read_built_from(text)
    return None, {}


def commits_between(owner, name, old, new):
    """(commits, status): the commits from old (exclusive) to new, oldest first, as (sha, message)."""
    data = json.loads(gh("api", "repos/%s/%s/compare/%s...%s?per_page=%d" % (owner, name, old, new, COMPARE_MAX)))
    commits = [(c["sha"], c["commit"]["message"]) for c in data.get("commits", [])]
    return commits, data.get("status", ""), data.get("total_commits", len(commits))


def classify(message):
    subject = message.splitlines()[0].strip() if message.strip() else ""
    breaking = "BREAKING CHANGE:" in message or "BREAKING-CHANGE:" in message
    m = SUBJECT.match(subject)
    if not m:
        return None, subject, breaking
    breaking = breaking or bool(m.group("bang"))
    text = m.group("text")
    if m.group("scope"):
        text = "**%s:** %s" % (m.group("scope"), text)
    return m.group("type").lower(), text, breaking


def section(owner, name, old, new):
    out = ["### %s" % name, ""]
    try:
        commits, status, total = commits_between(owner, name, old, new)
    except RuntimeError as e:
        return out + ["The changes could not be listed (%s): %s...%s" % (e, old[:9], new[:9]), ""]
    if status == "behind":
        return out + ["Went back to an earlier commit: %s (was %s)." % (new[:9], old[:9]), ""]
    if status == "diverged":
        out += ["The new commit is not a descendant of the old one (%s -> %s); the commits on the new side:" %
                (old[:9], new[:9]), ""]
    groups = {h: [] for h, _ in TYPES}
    breaking = []
    for sha, message in commits:
        kind, text, brk = classify(message)
        line = "- %s ([%s](https://github.com/%s/%s/commit/%s))" % (text, sha[:7], owner, name, sha)
        if brk:
            breaking.append(line)
        for heading, kinds in TYPES:
            if kinds is None or kind in kinds:
                groups[heading].append(line)
                break
    if breaking:
        out += ["**Breaking changes**", ""] + breaking + [""]
    for heading, _ in TYPES:
        if groups[heading]:
            out += ["**%s**" % heading, ""] + groups[heading] + [""]
    if total > len(commits):
        out += ["(%d more commits not listed: https://github.com/%s/%s/compare/%s...%s)" %
                (total - len(commits), owner, name, old[:12], new[:12]), ""]
    return out


def notes(repo, version, pins, extra=""):
    owner = repo.split("/")[0]
    # the board's own text (how to run the images): first, before the list of changes
    out = ([extra.strip(), ""] if extra.strip() else []) + ["## What changed", ""]
    try:
        prev, old_pins = previous_release(repo, version)
    except RuntimeError as e:  # the notes never stop a release
        prev, old_pins = "", {}
        out += ["The previous release could not be read (%s), so the changes are not listed." % e, ""]
    if prev == "":
        pass
    elif prev is None:
        out += ["The first release.", ""]
    else:
        out += ["Since %s." % prev, ""]
        changed = [r for r in pins if r in old_pins and old_pins[r] != pins[r]]
        added = [r for r in pins if r not in old_pins]
        dropped = [r for r in old_pins if r not in pins]
        if not (changed or added or dropped):
            out += ["Built from the same commits as %s." % prev, ""]
        for r in changed:
            out += section(owner, r, old_pins[r], pins[r])
        for r in added:
            out += ["### %s" % r, "", "New in this release (at %s)." % pins[r][:9], ""]
        for r in dropped:
            out += ["### %s" % r, "", "No longer part of this release.", ""]
    out += ["## Built from (repository commit)", "", "```"] + ["%s %s" % (r, c) for r, c in pins.items()] + ["```", ""]
    return "\n".join(out)


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--repo", required=True, help="owner/name of the repository being released")
    ap.add_argument("--version", default="", help="the tag this release gets (left out: it does not exist yet)")
    ap.add_argument("--built-from", required=True, help="the release's built-from.txt")
    ap.add_argument("--extra", help="a Markdown file of the board's own text, put before the changes (.github/release-notes.md)")
    ap.add_argument("--out", help="write the notes here (default: stdout)")
    a = ap.parse_args()
    with open(a.built_from, encoding="utf-8") as f:
        pins = read_built_from(f.read())
    if not pins:
        sys.exit("release_notes: no '<repository> <commit>' lines in %s" % a.built_from)
    extra = ""
    if a.extra and os.path.isfile(a.extra):
        with open(a.extra, encoding="utf-8") as f:
            extra = f.read()
    text = notes(a.repo, a.version, pins, extra)
    if a.out:
        with open(a.out, "w", encoding="utf-8", newline="\n") as f:
            f.write(text)
    else:
        sys.stdout.write(text)


if __name__ == "__main__":
    main()
