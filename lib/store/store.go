// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package store keeps the history: every log line, parsed event, boot, op call (who asked, what, result) in
// SQLite, and blobs (images, screenshots, dumps) as files under blobs/ named by sha256.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS boots (
	id INTEGER PRIMARY KEY, device TEXT NOT NULL, started INTEGER NOT NULL,
	slot TEXT NOT NULL DEFAULT '', outcome TEXT NOT NULL DEFAULT '', pinned INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS boots_dev ON boots(device, id);
CREATE TABLE IF NOT EXISTS lines (
	id INTEGER PRIMARY KEY, device TEXT NOT NULL, boot INTEGER NOT NULL, ts INTEGER NOT NULL, text TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS lines_dev ON lines(device, id);
CREATE INDEX IF NOT EXISTS lines_boot ON lines(boot, id);
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY, device TEXT NOT NULL, boot INTEGER NOT NULL, ts INTEGER NOT NULL,
	kind TEXT NOT NULL, data TEXT NOT NULL DEFAULT '{}');
CREATE INDEX IF NOT EXISTS events_dev ON events(device, id);
CREATE INDEX IF NOT EXISTS events_kind ON events(kind, id);
CREATE TABLE IF NOT EXISTS ops (
	id INTEGER PRIMARY KEY, device TEXT NOT NULL, ts INTEGER NOT NULL, caller TEXT NOT NULL, op TEXT NOT NULL,
	args TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', ms INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS ops_dev ON ops(device, id);
CREATE TABLE IF NOT EXISTS blobs (
	sha TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, size INTEGER NOT NULL, ts INTEGER NOT NULL,
	device TEXT NOT NULL DEFAULT '', boot INTEGER NOT NULL DEFAULT 0, meta TEXT NOT NULL DEFAULT '{}',
	pinned INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS blobs_kind ON blobs(kind, ts);
CREATE TABLE IF NOT EXISTS prefs (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

type Store struct {
	db  *sql.DB
	dir string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "phonelab.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; reads are short
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db, dir: dir}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Dir() string  { return s.dir }

func ms(t time.Time) int64 { return t.UnixMilli() }

// ---- boots

type Boot struct {
	ID      int64  `json:"id"`
	Device  string `json:"device"`
	Started int64  `json:"started"`
	Slot    string `json:"slot"`
	Outcome string `json:"outcome"`
	Pinned  bool   `json:"pinned"`
	Lines   int64  `json:"lines"`
	Faults  int64  `json:"faults"`
}

func (s *Store) NewBoot(dev string, t time.Time) (int64, error) {
	r, err := s.db.Exec(`INSERT INTO boots(device, started) VALUES(?, ?)`, dev, ms(t))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// LastBoot returns the newest boot id of dev (0 if none) so a restarted service keeps appending to it.
func (s *Store) LastBoot(dev string) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(id) FROM boots WHERE device=?`, dev).Scan(&id)
	return id.Int64, err
}

func (s *Store) SetBoot(id int64, field, value string) error {
	if field != "slot" && field != "outcome" {
		return fmt.Errorf("bad boot field %q", field)
	}
	_, err := s.db.Exec(`UPDATE boots SET `+field+`=? WHERE id=?`, value, id)
	return err
}

func (s *Store) Boots(dev string, before int64, limit int) ([]Boot, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.db.Query(`SELECT b.id, b.device, b.started, b.slot, b.outcome, b.pinned,
		(SELECT COUNT(*) FROM lines l WHERE l.boot=b.id),
		(SELECT COUNT(*) FROM events e WHERE e.boot=b.id AND e.kind IN ('fault','panic'))
		FROM boots b WHERE b.device=? AND b.id<? ORDER BY b.id DESC LIMIT ?`, dev, before, clamp(limit, 50, 1000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Boot
	for rows.Next() {
		var b Boot
		if err := rows.Scan(&b.ID, &b.Device, &b.Started, &b.Slot, &b.Outcome, &b.Pinned, &b.Lines, &b.Faults); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---- lines

type Line struct {
	ID     int64  `json:"id"`
	Device string `json:"device"`
	Boot   int64  `json:"boot"`
	TS     int64  `json:"ts"`
	Text   string `json:"text"`
}

// AddLines inserts a batch in one transaction and fills in the ids.
func (s *Store) AddLines(batch []Line) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	st, err := tx.Prepare(`INSERT INTO lines(device, boot, ts, text) VALUES(?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	for i := range batch {
		r, err := st.Exec(batch[i].Device, batch[i].Boot, batch[i].TS, batch[i].Text)
		if err != nil {
			st.Close()
			tx.Rollback()
			return err
		}
		batch[i].ID, _ = r.LastInsertId()
	}
	st.Close()
	return tx.Commit()
}

// LineQuery selects lines. Grep is a case-insensitive substring. Before/After page by id. Results are in
// ascending id order; with Before (or neither) the newest Limit lines are returned.
type LineQuery struct {
	Device string
	Boot   int64
	After  int64
	Before int64
	Grep   string
	// Any: at least one of these substrings; Exclude: none of them (both case-insensitive, as Grep)
	Any     []string
	Exclude []string
	Limit   int
}

func (s *Store) Lines(q LineQuery) ([]Line, error) {
	where := []string{"device=?"}
	args := []any{q.Device}
	if q.Boot > 0 {
		where = append(where, "boot=?")
		args = append(args, q.Boot)
	}
	if q.After > 0 {
		where = append(where, "id>?")
		args = append(args, q.After)
	}
	if q.Before > 0 {
		where = append(where, "id<?")
		args = append(args, q.Before)
	}
	if q.Grep != "" {
		where = append(where, "text LIKE ? ESCAPE '\\'")
		args = append(args, "%"+likeEscape(q.Grep)+"%")
	}
	if len(q.Any) > 0 {
		var or []string
		for _, g := range q.Any {
			or = append(or, "text LIKE ? ESCAPE '\\'")
			args = append(args, "%"+likeEscape(g)+"%")
		}
		where = append(where, "("+strings.Join(or, " OR ")+")")
	}
	for _, g := range q.Exclude {
		where = append(where, "text NOT LIKE ? ESCAPE '\\'")
		args = append(args, "%"+likeEscape(g)+"%")
	}
	order := "DESC"
	if q.After > 0 {
		order = "ASC"
	}
	args = append(args, clamp(q.Limit, 500, 10000))
	rows, err := s.db.Query(`SELECT id, device, boot, ts, text FROM lines WHERE `+strings.Join(where, " AND ")+
		` ORDER BY id `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Line
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.ID, &l.Device, &l.Boot, &l.TS, &l.Text); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if order == "DESC" {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---- events

type Event struct {
	ID     int64           `json:"id"`
	Device string          `json:"device"`
	Boot   int64           `json:"boot"`
	TS     int64           `json:"ts"`
	Kind   string          `json:"kind"`
	Data   json.RawMessage `json:"data"`
}

func (s *Store) AddEvent(dev string, boot int64, t time.Time, kind string, data any) (int64, error) {
	b, err := json.Marshal(data)
	if err != nil || string(b) == "null" {
		b = []byte("{}")
	}
	r, err := s.db.Exec(`INSERT INTO events(device, boot, ts, kind, data) VALUES(?,?,?,?,?)`, dev, boot, ms(t), kind, string(b))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

type EventQuery struct {
	Device string
	Boot   int64
	Kinds  []string // prefix match: "mon." matches mon.cpu
	Before int64
	Limit  int
}

func (s *Store) Events(q EventQuery) ([]Event, error) {
	where := []string{"device=?"}
	args := []any{q.Device}
	if q.Boot > 0 {
		where = append(where, "boot=?")
		args = append(args, q.Boot)
	}
	if q.Before > 0 {
		where = append(where, "id<?")
		args = append(args, q.Before)
	}
	if len(q.Kinds) > 0 {
		var or []string
		for _, k := range q.Kinds {
			if strings.HasSuffix(k, ".") {
				or = append(or, "kind LIKE ?")
				args = append(args, k+"%")
			} else {
				or = append(or, "kind=?")
				args = append(args, k)
			}
		}
		where = append(where, "("+strings.Join(or, " OR ")+")")
	}
	args = append(args, clamp(q.Limit, 200, 5000))
	rows, err := s.db.Query(`SELECT id, device, boot, ts, kind, data FROM events WHERE `+strings.Join(where, " AND ")+
		` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var d string
		if err := rows.Scan(&e.ID, &e.Device, &e.Boot, &e.TS, &e.Kind, &d); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- ops (the audit trail: every API/MCP call)

type Op struct {
	ID     int64           `json:"id"`
	Device string          `json:"device"`
	TS     int64           `json:"ts"`
	Caller string          `json:"caller"`
	Op     string          `json:"op"`
	Args   json.RawMessage `json:"args"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	MS     int64           `json:"ms"`
}

func (s *Store) AddOp(o Op) (int64, error) {
	if len(o.Args) == 0 {
		o.Args = json.RawMessage("{}")
	}
	r, err := s.db.Exec(`INSERT INTO ops(device, ts, caller, op, args, result, error, ms) VALUES(?,?,?,?,?,?,?,?)`,
		o.Device, o.TS, o.Caller, o.Op, string(o.Args), string(o.Result), o.Error, o.MS)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) Ops(dev string, before int64, limit int) ([]Op, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.db.Query(`SELECT id, device, ts, caller, op, args, result, error, ms FROM ops
		WHERE device=? AND id<? ORDER BY id DESC LIMIT ?`, dev, before, clamp(limit, 100, 2000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var o Op
		var a, r string
		if err := rows.Scan(&o.ID, &o.Device, &o.TS, &o.Caller, &o.Op, &a, &r, &o.Error, &o.MS); err != nil {
			return nil, err
		}
		o.Args = json.RawMessage(a)
		if r != "" {
			o.Result = json.RawMessage(r)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- blobs

type Blob struct {
	SHA    string          `json:"sha"`
	Kind   string          `json:"kind"`
	Name   string          `json:"name"`
	Size   int64           `json:"size"`
	TS     int64           `json:"ts"`
	Device string          `json:"device"`
	Boot   int64           `json:"boot"`
	Meta   json.RawMessage `json:"meta"`
	Pinned bool            `json:"pinned"`
}

func (s *Store) blobPath(sha string) string {
	return filepath.Join(s.dir, "blobs", sha[:2], sha)
}

func validSHA(sha string) bool {
	if len(sha) != 64 {
		return false
	}
	_, err := hex.DecodeString(sha)
	return err == nil
}

// PutBlob stores r (deduplicated by content) and records it.
func (s *Store) PutBlob(kind, name, dev string, boot int64, meta any, r io.Reader) (Blob, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "blobs"), "up-*")
	if err != nil {
		return Blob{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), r)
	tmp.Close()
	if err != nil {
		return Blob{}, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	p := s.blobPath(sha)
	if _, err := os.Stat(p); err != nil {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Blob{}, err
		}
		if err := os.Rename(tmp.Name(), p); err != nil {
			return Blob{}, err
		}
	}
	mb, _ := json.Marshal(meta)
	if string(mb) == "null" {
		mb = []byte("{}")
	}
	b := Blob{SHA: sha, Kind: kind, Name: name, Size: n, TS: ms(time.Now()), Device: dev, Boot: boot, Meta: mb}
	_, err = s.db.Exec(`INSERT INTO blobs(sha, kind, name, size, ts, device, boot, meta) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(sha) DO UPDATE SET ts=excluded.ts, name=excluded.name`,
		b.SHA, b.Kind, b.Name, b.Size, b.TS, b.Device, b.Boot, string(mb))
	return b, err
}

func (s *Store) OpenBlob(sha string) (io.ReadCloser, int64, error) {
	if !validSHA(sha) {
		return nil, 0, fmt.Errorf("bad blob id")
	}
	f, err := os.Open(s.blobPath(sha))
	if err != nil {
		return nil, 0, fmt.Errorf("blob %s: %w", sha[:12], err)
	}
	st, _ := f.Stat()
	return f, st.Size(), nil
}

func (s *Store) GetBlob(sha string) (Blob, error) {
	var b Blob
	var m string
	err := s.db.QueryRow(`SELECT sha, kind, name, size, ts, device, boot, meta, pinned FROM blobs WHERE sha=?`, sha).
		Scan(&b.SHA, &b.Kind, &b.Name, &b.Size, &b.TS, &b.Device, &b.Boot, &m, &b.Pinned)
	b.Meta = json.RawMessage(m)
	return b, err
}

func (s *Store) Blobs(kind string, limit int) ([]Blob, error) {
	q := `SELECT sha, kind, name, size, ts, device, boot, meta, pinned FROM blobs`
	var args []any
	if kind != "" {
		q += ` WHERE kind=?`
		args = append(args, kind)
	}
	args = append(args, clamp(limit, 100, 2000))
	rows, err := s.db.Query(q+` ORDER BY ts DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Blob
	for rows.Next() {
		var b Blob
		var m string
		if err := rows.Scan(&b.SHA, &b.Kind, &b.Name, &b.Size, &b.TS, &b.Device, &b.Boot, &m, &b.Pinned); err != nil {
			return nil, err
		}
		b.Meta = json.RawMessage(m)
		out = append(out, b)
	}
	return out, rows.Err()
}

// Pin marks a boot or blob as exempt from retention.
func (s *Store) Pin(what string, id string, on bool) error {
	switch what {
	case "boot":
		_, err := s.db.Exec(`UPDATE boots SET pinned=? WHERE id=?`, on, id)
		return err
	case "blob":
		_, err := s.db.Exec(`UPDATE blobs SET pinned=? WHERE sha=?`, on, id)
		return err
	}
	return fmt.Errorf("pin: unknown %q", what)
}

// Prune deletes unpinned boots (with their lines and events) older than maxAge. Zero = keep everything.
func (s *Store) Prune(maxAge time.Duration) (int64, error) {
	if maxAge <= 0 {
		return 0, nil
	}
	cut := ms(time.Now().Add(-maxAge))
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	sel := `SELECT id FROM boots WHERE pinned=0 AND started<? AND id NOT IN (SELECT MAX(id) FROM boots GROUP BY device)`
	for _, t := range []string{"lines", "events"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE boot IN (`+sel+`)`, cut); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	r, err := tx.Exec(`DELETE FROM boots WHERE id IN (`+sel+`)`, cut)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	n, _ := r.RowsAffected()
	return n, tx.Commit()
}

func (s *Store) GetPref(k string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM prefs WHERE key=?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetPref(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO prefs(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
