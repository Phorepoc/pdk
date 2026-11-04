// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package client talks to a running PhoneLab service over its REST API. The service owns each device's link, so a
// command-line tool (cmd/phe) runs device operations through it instead of opening the port itself.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
)

// Client: Base = "http://localhost:8737"; Token = the service's bearer token, if it has one.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func New(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 20 * time.Minute}}
}

// Error is a non-2xx answer; Result carries an op's partial result (422).
type Error struct {
	Status  int
	Message string
	Result  json.RawMessage
}

func (e *Error) Error() string { return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status) }

func (c *Client) do(method, path string, body any, out any, ok ...int) error {
	var rd io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, ct = bytes.NewReader(b), "application/json"
	}
	return c.send(method, path, rd, ct, out, ok...)
}

// send: one request with a ready body (JSON, raw bytes, multipart) and its content type.
func (c *Client) send(method, path string, rd io.Reader, contentType string, out any, ok ...int) error {
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Phonelab-Caller", "cli")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("PhoneLab at %s: %w", c.Base, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	good := res.StatusCode/100 == 2
	for _, s := range ok {
		good = good || res.StatusCode == s
	}
	if !good {
		var e struct {
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return &Error{Status: res.StatusCode, Message: e.Error, Result: e.Result}
	}
	if out == nil {
		return nil
	}
	if raw, isRaw := out.(*[]byte); isRaw {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

// Health of the service and every device (a device down answers 503 with the same body).
func (c *Client) Health() (core.HealthReport, error) {
	var h core.HealthReport
	err := c.do("GET", "/api/health", nil, &h, http.StatusServiceUnavailable)
	return h, err
}

// Ops a device offers.
func (c *Client) Ops(dev string) ([]device.OpSpec, error) {
	var o []device.OpSpec
	err := c.do("GET", "/api/devices/"+url.PathEscape(dev)+"/ops", nil, &o)
	return o, err
}

// Op runs an op and returns its JSON result.
func (c *Client) Op(dev, op string, args map[string]any) (json.RawMessage, error) {
	var r struct {
		Result json.RawMessage `json:"result"`
	}
	if args == nil {
		args = map[string]any{}
	}
	err := c.do("POST", "/api/devices/"+url.PathEscape(dev)+"/ops/"+url.PathEscape(op), args, &r)
	return r.Result, err
}

// Lines reads stored log lines (grep = substring, boot 0 = all, limit 0 = the service default).
func (c *Client) Lines(dev, grep string, boot int64, limit int) ([]store.Line, error) {
	q := url.Values{}
	if grep != "" {
		q.Set("grep", grep)
	}
	if boot > 0 {
		q.Set("boot", fmt.Sprint(boot))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	var l []store.Line
	err := c.do("GET", "/api/devices/"+url.PathEscape(dev)+"/lines?"+q.Encode(), nil, &l)
	return l, err
}

// Blob downloads a stored blob (a screenshot PNG ...).
func (c *Client) Blob(sha string) ([]byte, error) {
	var b []byte
	err := c.do("GET", "/api/blobs/"+url.PathEscape(sha), nil, &b)
	return b, err
}

func devPath(dev, rest string) string { return "/api/devices/" + url.PathEscape(dev) + rest }

// Device is one entry of the service's device list.
type Device struct {
	ID     string          `json:"id"`
	Driver string          `json:"driver"`
	Health core.Health     `json:"health"`
	Ops    []device.OpSpec `json:"ops"`
}

// Devices the service has, with driver and health.
func (c *Client) Devices() ([]Device, error) {
	var d []Device
	err := c.do("GET", "/api/devices", nil, &d)
	return d, err
}

// LineQuery filters stored log lines: Grep = substring, Any / Exclude = substrings separated by |, Boot -1 = the
// current boot, 0 = all, After / Before = line ids.
type LineQuery struct {
	Grep, Any, Exclude  string
	Boot, After, Before int64
	Limit               int
}

// LinesQ reads stored log lines with the full filter set of the REST API.
func (c *Client) LinesQ(dev string, q LineQuery) ([]store.Line, error) {
	v := url.Values{}
	for k, s := range map[string]string{"grep": q.Grep, "any": q.Any, "exclude": q.Exclude} {
		if s != "" {
			v.Set(k, s)
		}
	}
	for k, n := range map[string]int64{"boot": q.Boot, "after": q.After, "before": q.Before, "limit": int64(q.Limit)} {
		if n != 0 {
			v.Set(k, fmt.Sprint(n))
		}
	}
	var l []store.Line
	err := c.do("GET", devPath(dev, "/lines?"+v.Encode()), nil, &l)
	return l, err
}

// Boots lists recent boots (before = a boot id to page from, 0 = newest).
func (c *Client) Boots(dev string, before int64, limit int) ([]store.Boot, error) {
	var b []store.Boot
	err := c.do("GET", devPath(dev, fmt.Sprintf("/boots?before=%d&limit=%d", before, limit)), nil, &b)
	return b, err
}

// Events lists the structured events parsed from the log; kinds is a comma list (a trailing dot = prefix).
func (c *Client) Events(dev, kinds string, boot int64, limit int) ([]store.Event, error) {
	v := url.Values{"boot": {fmt.Sprint(boot)}, "limit": {fmt.Sprint(limit)}}
	if kinds != "" {
		v.Set("kind", kinds)
	}
	var e []store.Event
	err := c.do("GET", devPath(dev, "/events?"+v.Encode()), nil, &e)
	return e, err
}

// History lists the operations run on the device, newest first.
func (c *Client) History(dev string, before int64, limit int) ([]store.Op, error) {
	var o []store.Op
	err := c.do("GET", devPath(dev, fmt.Sprintf("/history?before=%d&limit=%d", before, limit)), nil, &o)
	return o, err
}

// FsListing is a directory on the device.
type FsListing struct {
	Path    string          `json:"path"`
	Entries json.RawMessage `json:"entries"`
}

// FsList lists a directory on the device.
func (c *Client) FsList(dev, path string) (FsListing, error) {
	var l FsListing
	err := c.do("GET", devPath(dev, "/fs?"+url.Values{"path": {path}}.Encode()), nil, &l)
	return l, err
}

// FsRead downloads a file from the device.
func (c *Client) FsRead(dev, path string) ([]byte, error) {
	var b []byte
	err := c.do("GET", devPath(dev, "/fs/content?"+url.Values{"path": {path}}.Encode()), nil, &b)
	return b, err
}

// FsWrite uploads r as a file on the device (the first chunk truncates).
func (c *Client) FsWrite(dev, path string, r io.Reader) (int64, error) {
	var w struct {
		Written int64 `json:"written"`
	}
	err := c.send("PUT", devPath(dev, "/fs/content?"+url.Values{"path": {path}}.Encode()), r, "application/octet-stream", &w)
	return w.Written, err
}

// FsDelete deletes a file on the device.
func (c *Client) FsDelete(dev, path string) error {
	return c.do("DELETE", devPath(dev, "/fs/content?"+url.Values{"path": {path}}.Encode()), nil, nil)
}

// PutBlob uploads r into the service's blob store (kind "image" for a push).
func (c *Client) PutBlob(kind, name, dev string, r io.Reader) (store.Blob, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", name)
		if err == nil {
			_, err = io.Copy(part, r)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	q := url.Values{"kind": {kind}}
	if dev != "" {
		q.Set("device", dev)
	}
	var b store.Blob
	err := c.send("POST", "/api/blobs?"+q.Encode(), pr, mw.FormDataContentType(), &b)
	return b, err
}

// Blobs lists stored blobs of a kind ("" = all).
func (c *Client) Blobs(kind string, limit int) ([]store.Blob, error) {
	var b []store.Blob
	err := c.do("GET", "/api/blobs?"+url.Values{"kind": {kind}, "limit": {fmt.Sprint(limit)}}.Encode(), nil, &b)
	return b, err
}

// Pin keeps (or releases) a boot or a blob against retention: what = "boot" or "blob".
func (c *Client) Pin(what, id string, on bool) error {
	return c.do("POST", "/api/pin", map[string]any{"what": what, "id": id, "on": on}, nil)
}
