// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Phorepoc/pdk/frontend/mcp"
	"github.com/Phorepoc/pdk/frontend/rest"
	"github.com/Phorepoc/pdk/lib/client"
	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/mock"
	"github.com/Phorepoc/pdk/lib/store"
)

// fsDriver: a device with an in-memory file service, to drive the fs routes the way the nokia driver does.
type fsDriver struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (d *fsDriver) Name() string { return "fake" }
func (d *fsDriver) Ops() []device.OpSpec {
	return []device.OpSpec{{Name: "fs_list"}, {Name: "fs_read"}, {Name: "fs_write"}, {Name: "fs_delete"}}
}
func (d *fsDriver) BootStart(string) bool          { return false }
func (d *fsDriver) Classify(string) []device.Event { return nil }
func (d *fsDriver) Exec(ctx context.Context, l device.Link, op string, a device.Args) (any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, _ := a.String("path")
	switch op {
	case "fs_list":
		var es []map[string]any
		for k, v := range d.files {
			if strings.HasPrefix(k, strings.TrimSuffix(p, "/")+"/") {
				es = append(es, map[string]any{"name": k, "size": len(v)})
			}
		}
		return map[string]any{"entries": es}, nil
	case "fs_read":
		b, ok := d.files[p]
		if !ok {
			return nil, errors.New("no such file")
		}
		off, _ := a.Uint("offset", 64)
		n, _ := a.Uint("len", 32)
		end := min(int(off)+int(n), len(b))
		return map[string]any{"data_b64": base64.StdEncoding.EncodeToString(b[off:end]), "eof": end == len(b)}, nil
	case "fs_write":
		raw, _ := a.String("data_b64")
		b, _ := base64.StdEncoding.DecodeString(raw)
		if t, _ := a["truncate"].(bool); t {
			d.files[p] = nil
		}
		d.files[p] = append(d.files[p], b...)
		return map[string]any{"written": len(b)}, nil
	case "fs_delete":
		delete(d.files, p)
		return map[string]any{}, nil
	}
	return nil, errors.New("unknown op")
}

func fsServer(t *testing.T) (*httptest.Server, *fsDriver) {
	t.Helper()
	lines, err := mock.LoadTranscript("../../testdata/boot-slotb-rearm.log", 50)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	drv := &fsDriver{files: map[string][]byte{}}
	m := core.NewManager(st)
	s := core.NewSession("fake", drv, mock.New(lines).Opener(), st, m.Bus)
	m.Add(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	m.Version, m.Started = "test", time.Now()
	ts := httptest.NewServer((&rest.Server{M: m}).Handler(map[string]http.Handler{"/mcp": mcp.Handler(m, "test")}))
	t.Cleanup(func() { ts.Close(); cancel(); <-done; st.Close() })
	c, cc := context.WithTimeout(ctx, 5*time.Second)
	defer cc()
	if err := s.WaitConnected(c); err != nil {
		t.Fatal(err)
	}
	return ts, drv
}

// The CLI's client reaches the device's file service, in chunks bigger than one.
func TestClientFiles(t *testing.T) {
	ts, drv := fsServer(t)
	c := client.New(ts.URL, "")
	data := bytes.Repeat([]byte("0123456789"), 1000) // several 3072-byte chunks
	n, err := c.FsWrite("fake", "/E:/a.bin", bytes.NewReader(data))
	if err != nil || n != int64(len(data)) {
		t.Fatalf("write %d %v", n, err)
	}
	got, err := c.FsRead("fake", "/E:/a.bin")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes, %v", len(got), err)
	}
	l, err := c.FsList("fake", "/E:")
	if err != nil || !strings.Contains(string(l.Entries), "a.bin") {
		t.Fatalf("list %+v %v", l, err)
	}
	if err := c.FsDelete("fake", "/E:/a.bin"); err != nil || len(drv.files) != 0 {
		t.Fatalf("delete %v %v", err, drv.files)
	}
	if _, err := c.FsRead("fake", "/E:/gone"); err == nil {
		t.Fatal("reading a missing file succeeded")
	}
}

// Everything the CLI reads from the service's record comes back through the client, and the blob store, pins and
// the device list are reachable from REST (client) and MCP alike.
func TestClientRecordBlobsPin(t *testing.T) {
	ts, _ := server(t, "")
	time.Sleep(400 * time.Millisecond)
	c := client.New(ts.URL, "")

	ds, err := c.Devices()
	if err != nil || len(ds) != 1 || ds[0].ID != "phone" || ds[0].Driver == "" {
		t.Fatalf("devices %+v %v", ds, err)
	}
	bs, err := c.Boots("phone", 0, 5)
	if err != nil || len(bs) == 0 {
		t.Fatalf("boots %+v %v", bs, err)
	}
	if _, err := c.Events("phone", "boot.", 0, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Op("phone", "peek", map[string]any{"addr": "0x100"}); err != nil {
		t.Fatal(err)
	}
	h, err := c.History("phone", 0, 10)
	if err != nil || len(h) != 1 || h[0].Op != "peek" || h[0].Caller != "cli" {
		t.Fatalf("history %+v %v", h, err)
	}
	all, _ := c.LinesQ("phone", client.LineQuery{Limit: 100})
	some, err := c.LinesQ("phone", client.LineQuery{Any: "phlink:|slot", Exclude: "rearm", Limit: 100})
	if err != nil || len(some) == 0 || len(some) >= len(all) {
		t.Fatalf("lines: %d of %d, %v", len(some), len(all), err)
	}

	img := bytes.Repeat([]byte{9, 8, 7}, 4000)
	b, err := c.PutBlob("image", "x.img", "phone", bytes.NewReader(img))
	if err != nil || b.Size != int64(len(img)) || len(b.SHA) != 64 {
		t.Fatalf("blob %+v %v", b, err)
	}
	if list, err := c.Blobs("image", 10); err != nil || len(list) != 1 || list[0].SHA != b.SHA {
		t.Fatalf("blobs %+v %v", list, err)
	}
	if got, err := c.Blob(b.SHA); err != nil || !bytes.Equal(got, img) {
		t.Fatalf("blob download %v", err)
	}
	if err := c.Pin("blob", b.SHA, true); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.Blobs("image", 10); !list[0].Pinned {
		t.Fatal("not pinned")
	}
	if err := c.Pin("nonsense", "1", true); err == nil {
		t.Fatal("bad pin kind accepted")
	}

	// the same through MCP: devices, blobs, blob_put, pin
	ctx := context.Background()
	cl := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil)
	sess, err := cl.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(name string, args map[string]any) string {
		t.Helper()
		r, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %v %+v", name, err, r)
		}
		return r.Content[0].(*sdk.TextContent).Text
	}
	if out := call("devices", nil); !strings.Contains(out, `"phone"`) {
		t.Fatalf("devices: %s", out)
	}
	var put store.Blob
	if err := json.Unmarshal([]byte(call("blob_put", map[string]any{"name": "m.img", "data_b64": base64.StdEncoding.EncodeToString(img)})), &put); err != nil || put.SHA != b.SHA {
		t.Fatalf("blob_put %+v %v (same bytes, same sha)", put, err)
	}
	if out := call("blobs", map[string]any{"kind": "image"}); !strings.Contains(out, b.SHA) {
		t.Fatalf("blobs: %s", out)
	}
	call("pin", map[string]any{"what": "blob", "id": b.SHA, "on": false})
	if list, _ := c.Blobs("image", 10); list[0].Pinned {
		t.Fatal("not released")
	}
}
