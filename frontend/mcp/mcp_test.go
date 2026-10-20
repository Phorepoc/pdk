// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Phorepoc/pdk/frontend/mcp"
	"github.com/Phorepoc/pdk/frontend/rest"
	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device/pinephone"
	"github.com/Phorepoc/pdk/lib/mock"
	"github.com/Phorepoc/pdk/lib/store"
)

// server runs the whole HTTP surface (API + MCP) against the mock phone.
func server(t *testing.T, token string) (*httptest.Server, *mock.Phone) {
	t.Helper()
	lines, err := mock.LoadTranscript("../../testdata/boot-slotb-rearm.log", 200)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ph := mock.New(lines)
	m := core.NewManager(st)
	s := core.NewSession("pinephone", &pinephone.Driver{}, ph.Opener(), st, m.Bus)
	m.Add(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	m.Version, m.Started = "test", time.Now()
	a := &rest.Server{M: m, Token: token}
	ts := httptest.NewServer(a.Handler(map[string]http.Handler{"/mcp": mcp.Handler(m, "test")}))
	t.Cleanup(func() { ts.Close(); cancel(); <-done; st.Close() })
	c, cc := context.WithTimeout(ctx, 5*time.Second)
	defer cc()
	if err := s.WaitConnected(c); err != nil {
		t.Fatal(err)
	}
	return ts, ph
}

func do(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: %v\n%s", method, url, err, b)
		}
	}
	return resp.StatusCode
}

func TestRESTSurface(t *testing.T) {
	ts, ph := server(t, "")
	ph.Mem[0x100] = 0xabc

	var h rest.HealthReport
	if c := do(t, "GET", ts.URL+"/api/health", "", &h); c != 200 || !h.OK || h.Devices[0].State != "connected" {
		t.Fatalf("health %d %+v", c, h)
	}
	var devs []rest.DeviceInfo
	do(t, "GET", ts.URL+"/api/devices", "", &devs)
	if len(devs) != 1 || len(devs[0].Ops) < 6 {
		t.Fatalf("devices %+v", devs)
	}
	var res struct {
		Result []pinephone.Word `json:"result"`
		Error  string           `json:"error"`
	}
	if c := do(t, "POST", ts.URL+"/api/devices/pinephone/ops/peek", `{"addr":"0x100","count":2}`, &res); c != 200 || res.Result[0].Value != "0x00000abc" {
		t.Fatalf("peek %d %+v", c, res)
	}
	var e map[string]any
	if c := do(t, "POST", ts.URL+"/api/devices/pinephone/ops/peek", `{}`, &e); c != 422 {
		t.Fatalf("missing arg: %d %v", c, e)
	}
	if c := do(t, "POST", ts.URL+"/api/devices/nope/ops/peek", `{}`, &e); c != 404 {
		t.Fatalf("bad device: %d", c)
	}

	time.Sleep(400 * time.Millisecond)
	var lines []store.Line
	do(t, "GET", ts.URL+"/api/devices/pinephone/lines?grep=phlink:+P&limit=10", "", &lines)
	if len(lines) != 2 {
		t.Fatalf("grep lines %+v", lines)
	}
	var ops []store.Op
	do(t, "GET", ts.URL+"/api/devices/pinephone/history", "", &ops)
	if len(ops) != 1 || ops[0].Op != "peek" { // rejected calls (missing args) are not device ops
		t.Fatalf("history %+v", ops)
	}

	// upload an image as a blob, then push it by sha
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "symbian.img")
	fw.Write(bytes.Repeat([]byte{1, 2, 3, 4}, 5000))
	mw.Close()
	resp, err := http.Post(ts.URL+"/api/blobs?kind=image", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	var b store.Blob
	json.NewDecoder(resp.Body).Decode(&b)
	resp.Body.Close()
	if b.Size != 20000 || len(b.SHA) != 64 {
		t.Fatalf("blob %+v", b)
	}
	var pr struct {
		Result pinephone.PushResult `json:"result"`
	}
	if c := do(t, "POST", ts.URL+"/api/devices/pinephone/ops/push", `{"blob":"`+b.SHA+`","reboot":false}`, &pr); c != 200 || pr.Result.Bytes != 20000 || pr.Result.Rebooted {
		t.Fatalf("push %d %+v", c, pr)
	}
}

func TestLiveFrame(t *testing.T) {
	ts, _ := server(t, "")
	resp, err := http.Get(ts.URL + "/api/devices/pinephone/frame?which=2")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Frame-Height") != "720" || len(b) < 100 {
		t.Fatalf("frame %d %v %d bytes", resp.StatusCode, resp.Header, len(b))
	}
	var ops []store.Op
	do(t, "GET", ts.URL+"/api/devices/pinephone/history", "", &ops)
	if len(ops) != 0 {
		t.Fatalf("live frames must not fill the op history: %+v", ops)
	}
	var blobs []store.Blob
	do(t, "GET", ts.URL+"/api/blobs?kind=screenshot", "", &blobs)
	if len(blobs) != 0 {
		t.Fatalf("live frames must not be stored: %+v", blobs)
	}
	resp, _ = http.Get(ts.URL + "/api/devices/pinephone/frame?which=0&store=1")
	resp.Body.Close()
	if resp.Header.Get("X-Blob") == "" || resp.Header.Get("X-Frame-Height") != "640" {
		t.Fatalf("store=1 frame not stored: %v", resp.Header)
	}
}

func TestAuth(t *testing.T) {
	ts, _ := server(t, "s3cret")
	if c := do(t, "GET", ts.URL+"/api/health", "", nil); c != 401 {
		t.Fatalf("no token: %d", c)
	}
	if c := do(t, "GET", ts.URL+"/api/health?token=s3cret", "", nil); c != 200 {
		t.Fatalf("token: %d", c)
	}
}

func TestStream(t *testing.T) {
	ts, ph := server(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[4:]+"/api/devices/pinephone/stream?types=line,event", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(b), `"hello"`) {
		t.Fatalf("hello: %s %v", b, err)
	}
	ph.Say("[EKERN][key] power down")
	got := map[string]bool{}
	for !(got["line"] && got["event"]) {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var m core.Msg
		json.Unmarshal(b, &m)
		if strings.Contains(string(b), "power") {
			got[m.Type] = true
		}
	}
}

func TestMCPTools(t *testing.T) {
	ts, ph := server(t, "")
	ph.Mem[0x200] = 7
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cl := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	sess, err := cl.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, n := range []string{"push", "peek", "poke", "reboot", "rearm", "wait_for", "health", "log_lines", "events", "boots"} {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	r, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: "peek", Arguments: map[string]any{"addr": "0x200"}})
	if err != nil || r.IsError {
		t.Fatalf("peek: %v %+v", err, r)
	}
	if txt := r.Content[0].(*sdk.TextContent).Text; !strings.Contains(txt, "0x00000007") {
		t.Fatalf("peek text %s", txt)
	}
	r, _ = sess.CallTool(ctx, &sdk.CallToolParams{Name: "log_lines", Arguments: map[string]any{"grep": "phlink: P"}})
	time.Sleep(400 * time.Millisecond)
	r, _ = sess.CallTool(ctx, &sdk.CallToolParams{Name: "log_lines", Arguments: map[string]any{"grep": "phlink: P"}})
	if txt := r.Content[0].(*sdk.TextContent).Text; !strings.Contains(txt, "phlink: P 00000200 = 00000007") {
		t.Fatalf("log_lines %s", txt)
	}
	r, _ = sess.CallTool(ctx, &sdk.CallToolParams{Name: "rearm", Arguments: map[string]any{}})
	if !r.IsError {
		t.Fatal("rearm with empty slot B should be an error result")
	}
}
