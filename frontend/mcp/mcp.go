// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package mcp exposes the same operations as the REST API as MCP tools (streamable HTTP at /mcp). Tools are
// generated from the devices' op specs, plus read-only history tools. Every tool takes an optional "device"
// (defaults to the only device when there is one).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
)

func schemaType(t string) map[string]any {
	switch t {
	case "int":
		return map[string]any{"type": "integer"}
	case "bool":
		return map[string]any{"type": "boolean"}
	case "uint32":
		return map[string]any{"type": []string{"integer", "string"}}
	}
	return map[string]any{"type": "string"}
}

func deviceProp() map[string]any {
	return map[string]any{"type": "string", "description": "device id (optional when only one device is configured)"}
}

func objSchema(props map[string]any, required []string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

type handler struct{ mgr *core.Manager }

func (h handler) pick(args map[string]any) (*core.Session, error) {
	if id, _ := args["device"].(string); id != "" {
		if s := h.mgr.Get(id); s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("no device %q", id)
	}
	all := h.mgr.All()
	if len(all) == 1 {
		return all[0], nil
	}
	var ids []string
	for _, s := range all {
		ids = append(ids, s.ID)
	}
	return nil, fmt.Errorf("several devices; pass device (one of %s)", strings.Join(ids, ", "))
}

func result(v any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		txt := err.Error()
		if v != nil {
			b, _ := json.MarshalIndent(v, "", " ")
			txt += "\n" + string(b)
		}
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: txt}}}, nil
	}
	b, _ := json.MarshalIndent(v, "", " ")
	if v == nil {
		b = []byte(`"ok"`)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
}

func args(req *mcp.CallToolRequest) map[string]any {
	m := map[string]any{}
	if req.Params != nil && len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &m)
	}
	return m
}

func num(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case string:
		var n int64
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}

// NewServer builds the MCP server for the current device set.
// NewServer: the MCP front end over the library; version is reported to clients.
func NewServer(mgr *core.Manager, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "phonelab", Version: version}, &mcp.ServerOptions{
		Instructions: "PhoneLab owns the phone's debug link. Use these tools for every phone action (never open the " +
			"serial port yourself). Typical loop: push (image -> slot B, reboots), wait_for a boot line, log_lines " +
			"with grep to read what happened, peek/poke registers. History tools read the stored record of every boot.",
	})
	h := handler{mgr}

	// op tools: the union of every device's ops
	specs := map[string]device.OpSpec{}
	for _, s := range mgr.All() {
		for _, o := range s.Ops() {
			if _, ok := specs[o.Name]; !ok {
				specs[o.Name] = o
			}
		}
	}
	names := make([]string, 0, len(specs))
	for n := range specs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		o := specs[n]
		props := map[string]any{"device": deviceProp()}
		var req []string
		for _, p := range o.Params {
			sc := schemaType(p.Type)
			sc["description"] = p.Description
			props[p.Name] = sc
			if p.Required {
				req = append(req, p.Name)
			}
		}
		desc := o.Description
		if o.Reboots {
			desc += " (The phone resets: the link drops and comes back; pass wait to get the new boot's report, or use wait_boot.)"
		}
		hasBlob := o.Name == "screenshot"
		if hasBlob {
			props["inline"] = map[string]any{"type": "boolean", "description": "return the picture itself (an image) besides its blob id"}
		}
		srv.AddTool(&mcp.Tool{Name: o.Name, Description: desc, InputSchema: objSchema(props, req)},
			func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				m := args(r)
				s, err := h.pick(m)
				if err != nil {
					return result(nil, err)
				}
				delete(m, "device")
				inline, _ := m["inline"].(bool)
				delete(m, "inline")
				res, err := mgr.Exec(ctx, s, "mcp", o.Name, device.Args(m))
				out, rerr := result(res, err)
				if err == nil && hasBlob && inline {
					if img := blobImage(mgr.Store, res); img != nil {
						out.Content = append(out.Content, img)
					}
				}
				return out, rerr
			})
	}

	srv.AddTool(&mcp.Tool{Name: "health", Description: "Service and device health: link state, port, current boot, slot, " +
		"last byte age, reconnects, latest monitor stats (CPU, free RAM, SD I/O), the op in progress.",
		InputSchema: objSchema(map[string]any{}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return result(mgr.Health(), nil)
		})

	srv.AddTool(&mcp.Tool{Name: "ops_list", Description: "The operations a device supports, with parameters.",
		InputSchema: objSchema(map[string]any{"device": deviceProp()}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			s, err := h.pick(args(r))
			if err != nil {
				return result(nil, err)
			}
			return result(s.Ops(), nil)
		})

	srv.AddTool(&mcp.Tool{Name: "log_lines", Description: "Read stored log lines. Newest `limit` (default 200, max 2000) " +
		"unless `after` is given (then the lines after that id, oldest first). `grep` = case-insensitive substring; " +
		"`any` = several substrings separated by | (a line needs one); `exclude` = substrings separated by | to drop. " +
		"`boot` = a boot id from boots (0 = all, -1 = the current boot).",
		InputSchema: objSchema(map[string]any{"device": deviceProp(),
			"grep": map[string]any{"type": "string"}, "boot": map[string]any{"type": "integer"},
			"any": map[string]any{"type": "string"}, "exclude": map[string]any{"type": "string"},
			"after": map[string]any{"type": "integer"}, "before": map[string]any{"type": "integer"},
			"limit": map[string]any{"type": "integer"}}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			s, err := h.pick(m)
			if err != nil {
				return result(nil, err)
			}
			lim := int(num(m, "limit"))
			if lim <= 0 {
				lim = 200
			}
			boot := num(m, "boot")
			if boot < 0 {
				boot = s.Health().Boot
			}
			lines, err := mgr.Store.Lines(store.LineQuery{Device: s.ID, Boot: boot, After: num(m, "after"),
				Before: num(m, "before"), Grep: fmt.Sprint(orEmpty(m["grep"])), Any: splitBar(m["any"]),
				Exclude: splitBar(m["exclude"]), Limit: min(lim, 2000)})
			if err != nil {
				return result(nil, err)
			}
			// compact text: "id boot text" per line keeps tokens down
			var b strings.Builder
			for _, l := range lines {
				fmt.Fprintf(&b, "%d b%d %s\n", l.ID, l.Boot, l.Text)
			}
			if b.Len() == 0 {
				b.WriteString("(no lines)")
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: b.String()}}}, nil
		})

	srv.AddTool(&mcp.Tool{Name: "events", Description: "Structured events parsed from the log (boot.start, boot.slot, " +
		"fault, panic, upload.verdict, link.connected/disconnected, mon.cpu/ram/sdio, key.power...). `kind` is a " +
		"comma list; a trailing dot is a prefix (\"mon.\").",
		InputSchema: objSchema(map[string]any{"device": deviceProp(), "kind": map[string]any{"type": "string"},
			"boot": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			s, err := h.pick(m)
			if err != nil {
				return result(nil, err)
			}
			var kinds []string
			if k, _ := m["kind"].(string); k != "" {
				kinds = strings.Split(k, ",")
			}
			return result(mgr.Store.Events(store.EventQuery{Device: s.ID, Boot: num(m, "boot"), Kinds: kinds, Limit: int(num(m, "limit"))}))
		})

	srv.AddTool(&mcp.Tool{Name: "boots", Description: "Recent boots: id, start time, slot, outcome, line + fault counts.",
		InputSchema: objSchema(map[string]any{"device": deviceProp(), "limit": map[string]any{"type": "integer"}}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			s, err := h.pick(m)
			if err != nil {
				return result(nil, err)
			}
			return result(mgr.Store.Boots(s.ID, 0, int(num(m, "limit"))))
		})

	srv.AddTool(&mcp.Tool{Name: "op_history", Description: "Recent operations run on a device (by API, MCP or web), with args, result, error, duration.",
		InputSchema: objSchema(map[string]any{"device": deviceProp(), "limit": map[string]any{"type": "integer"}}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			s, err := h.pick(m)
			if err != nil {
				return result(nil, err)
			}
			return result(mgr.Store.Ops(s.ID, 0, int(num(m, "limit"))))
		})
	return srv
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

// Handler is the streamable-HTTP MCP endpoint.
func Handler(mgr *core.Manager, version string) http.Handler {
	srv := NewServer(mgr, version)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
}

// splitBar: "a|b|c" -> [a b c] (empty parts dropped).
func splitBar(v any) []string {
	t, _ := v.(string)
	var out []string
	for _, p := range strings.Split(t, "|") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// blobImage: the PNG a result names in its "blob" field, as MCP image content (nil when there is none).
func blobImage(st *store.Store, res any) mcp.Content {
	b, _ := json.Marshal(res)
	var r struct {
		Blob string `json:"blob"`
	}
	if json.Unmarshal(b, &r) != nil || r.Blob == "" {
		return nil
	}
	rc, _, err := st.OpenBlob(r.Blob)
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil
	}
	return &mcp.ImageContent{Data: data, MIMEType: "image/png"}
}
