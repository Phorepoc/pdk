// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package phlink

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// The push gate checks what actually went wrong on 2026-10-28: an image built from a STALE base (a private tree without
// fixes that were already published). Local changes on top of a current base are a normal experiment: they are recorded
// in the build record (and so in the push result), not refused. Whether a result counts is phone_gate's question (it
// wants a pipeline image without local changes).
//
// A base is current when, for every repository the board's public board.lock pins, the image's commit contains that
// pin (it is the pin or a descendant of it).

// baseSource answers the questions the check needs from the public repositories.
type baseSource interface {
	// BoardLock returns repository -> pinned commit from the public board.lock of the board repository repo.
	BoardLock(repo string) (map[string]string, error)
	// Contains reports whether commit head contains commit pin in repository repo (pin == head, or pin is an ancestor).
	Contains(repo, pin, head string) (bool, error)
}

// checkBase returns an error naming every repository whose commit in the record lacks its pinned commit.
func checkBase(p *Provenance, src baseSource) error {
	if p.Board == "" || p.Board == "?" {
		return fmt.Errorf("the build record names no board, so its base cannot be checked")
	}
	// the board's repository: the record's repository named "<something>-<board>"
	boardRepo := ""
	for r := range p.Repos {
		if strings.HasSuffix(r, "-"+p.Board) {
			boardRepo = r
		}
	}
	if boardRepo == "" {
		return fmt.Errorf("the build record lists no repository of board %s, so its base cannot be checked", p.Board)
	}
	lock, err := src.BoardLock(boardRepo)
	if err != nil {
		return fmt.Errorf("cannot read the %s board.lock to check the image's base: %v", p.Board, err)
	}
	var stale []string
	repos := make([]string, 0, len(lock))
	for r := range lock {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	for _, r := range repos {
		if r == boardRepo {
			continue // the release builds the board repository at its own commit; an old self-pin says nothing
		}
		pin := lock[r]
		have, ok := p.Repos[r]
		if !ok {
			stale = append(stale, fmt.Sprintf("%s (not in the build record)", r))
			continue
		}
		have = strings.TrimSuffix(have, "+dirty")
		in, err := src.Contains(r, pin, have)
		if err != nil {
			return fmt.Errorf("cannot check %s %s against its pin %s: %v", r, short(have), short(pin), err)
		}
		if !in {
			stale = append(stale, fmt.Sprintf("%s %s lacks the published %s", r, short(have), short(pin)))
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("stale base: %s", strings.Join(stale, "; "))
	}
	return nil
}

func short(c string) string {
	if len(c) > 9 {
		return c[:9]
	}
	return c
}

// githubBase reads the public Phorepoc repositories through the GitHub REST API (GITHUB_TOKEN or GH_TOKEN, if set,
// raises the rate limit).
type githubBase struct {
	owner string
	hc    *http.Client
}

func newGithubBase() *githubBase {
	return &githubBase{owner: "Phorepoc", hc: &http.Client{Timeout: 20 * time.Second}}
}

func (g *githubBase) get(path, accept string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/"+g.owner+"/"+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if t := g.token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return b, resp.StatusCode, err
}

// token: GITHUB_TOKEN / GH_TOKEN, else the GitHub CLI's login (gh auth token), else none (anonymous: 60 requests an hour)
func (g *githubBase) token() string {
	for _, v := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if t := os.Getenv(v); t != "" {
			return t
		}
	}
	ghTokenOnce.Do(func() {
		c := exec.Command("gh", "auth", "token")
		hideWindow(c) // (PhoneLab runs without a console: no window flash)
		if out, err := c.Output(); err == nil {
			ghToken = strings.TrimSpace(string(out))
		}
	})
	return ghToken
}

var (
	ghTokenOnce sync.Once
	ghToken     string
)

func (g *githubBase) BoardLock(repo string) (map[string]string, error) {
	b, code, err := g.get(repo+"/contents/board.lock?ref=main", "application/vnd.github.raw")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("HTTP %d", code)
	}
	return parseLock(string(b)), nil
}

func parseLock(s string) map[string]string {
	lock := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if f := strings.Fields(line); len(f) == 2 {
			lock[f[0]] = f[1]
		}
	}
	return lock
}

func (g *githubBase) Contains(repo, pin, head string) (bool, error) {
	if strings.HasPrefix(head, pin) || strings.HasPrefix(pin, head) {
		return true, nil
	}
	b, code, err := g.get(repo+"/compare/"+pin+"..."+head, "application/vnd.github+json")
	if err != nil {
		return false, err
	}
	if code == 404 {
		return false, fmt.Errorf("commit %s is not on GitHub (push it first)", short(head))
	}
	if code != 200 {
		return false, fmt.Errorf("HTTP %d", code)
	}
	var r struct {
		Status   string `json:"status"`
		BehindBy int    `json:"behind_by"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return false, err
	}
	// status of head relative to pin: "identical" / "ahead" = head contains pin; "behind" / "diverged" = it does not
	return r.BehindBy == 0 && (r.Status == "identical" || r.Status == "ahead"), nil
}
