// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Phorepoc/pdk/lib/core"
)

// maxBlobPut bounds what blob_put takes inline (base64 in a tool call); larger images go through POST /api/blobs
// or "phe device upload".
const maxBlobPut = 8 << 20

// addStoreTools: the service-level tools that are not device ops: the device list, the blob store, pins. They mirror
// REST GET /api/devices, GET+POST /api/blobs and POST /api/pin.
func addStoreTools(srv *mcp.Server, mgr *core.Manager, h handler) {
	srv.AddTool(&mcp.Tool{Name: "devices", Description: "The devices this service has: id, driver, health.",
		InputSchema: objSchema(map[string]any{}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			type info struct {
				ID     string      `json:"id"`
				Driver string      `json:"driver"`
				Health core.Health `json:"health"`
			}
			out := []info{}
			for _, s := range mgr.All() {
				out = append(out, info{s.ID, s.Driver.Name(), s.Health()})
			}
			return result(out, nil)
		})

	srv.AddTool(&mcp.Tool{Name: "blobs", Description: "Stored blobs (uploaded images, screenshots) by sha256; pass a sha256 as push's blob argument.",
		InputSchema: objSchema(map[string]any{"kind": map[string]any{"type": "string", "description": "image, screenshot ... (default: all)"},
			"limit": map[string]any{"type": "integer"}}, nil)},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			kind, _ := m["kind"].(string)
			return result(mgr.Store.Blobs(kind, int(num(m, "limit"))))
		})

	srv.AddTool(&mcp.Tool{Name: "blob_put", Description: fmt.Sprintf("Store a file in the blob store (up to %d MiB, base64) and get its sha256; large images: POST /api/blobs or `phe device upload`.", maxBlobPut>>20),
		InputSchema: objSchema(map[string]any{"device": deviceProp(),
			"name":     map[string]any{"type": "string", "description": "file name"},
			"data_b64": map[string]any{"type": "string", "description": "the file, base64"},
			"kind":     map[string]any{"type": "string", "description": "default image"}}, []string{"name", "data_b64"})},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			name, _ := m["name"].(string)
			enc, _ := m["data_b64"].(string)
			if len(enc) > maxBlobPut*4/3+4 {
				return result(nil, fmt.Errorf("blob_put: over %d MiB", maxBlobPut>>20))
			}
			data, err := base64.StdEncoding.DecodeString(enc)
			if err != nil || name == "" {
				return result(nil, fmt.Errorf("blob_put: a name and valid base64 data"))
			}
			kind, _ := m["kind"].(string)
			if kind == "" {
				kind = "image"
			}
			dev := ""
			if s, err := h.pick(m); err == nil {
				dev = s.ID
			}
			return result(mgr.Store.PutBlob(kind, name, dev, 0, map[string]any{"source": "mcp"}, bytes.NewReader(data)))
		})

	srv.AddTool(&mcp.Tool{Name: "pin", Description: "Keep a boot (id) or a blob (sha256) past the retention period, or release it (on=false).",
		InputSchema: objSchema(map[string]any{"what": map[string]any{"type": "string", "description": "boot or blob"},
			"id": map[string]any{"type": "string"}, "on": map[string]any{"type": "boolean", "description": "default true"}},
			[]string{"what", "id"})},
		func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := args(r)
			what, _ := m["what"].(string)
			id := fmt.Sprint(m["id"])
			on := true
			if v, ok := m["on"].(bool); ok {
				on = v
			}
			if err := mgr.Store.Pin(what, id, on); err != nil {
				return result(nil, err)
			}
			return result(map[string]bool{"ok": true}, nil)
		})
}
