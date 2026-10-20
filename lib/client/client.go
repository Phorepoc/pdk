// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package client talks to a running PhoneLab service over its REST API. The service owns each device's link, so a
// command-line tool (cmd/ph) runs device operations through it instead of opening the port itself.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Phonelab-Caller", "cli")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
