// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phonelab: the resident phone service. One process owns each phone's debug link and exposes it over
// REST + WebSocket (/api), MCP (/mcp) and the web UI (/). See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Phorepoc/pdk/frontend/mcp"
	"github.com/Phorepoc/pdk/frontend/rest"
	"github.com/Phorepoc/pdk/lib/config"
	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	_ "github.com/Phorepoc/pdk/lib/device/nokia"
	_ "github.com/Phorepoc/pdk/lib/device/pinephone"
	"github.com/Phorepoc/pdk/lib/mock"
	"github.com/Phorepoc/pdk/lib/store"
	"github.com/Phorepoc/pdk/lib/transport"
	webui "github.com/Phorepoc/pdk/web/embed"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "phonelab.toml", "config file (missing = defaults)")
	listen := flag.String("listen", "", "override listen address")
	dataDir := flag.String("data", "", "override data directory")
	mockFile := flag.String("mock", "", "run every device against the fake phone, replaying this transcript")
	logFile := flag.String("log", "", "append the service log to this file (the windowless Windows build has no console)")
	flag.Parse()
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if cfg.Workspace == "" {
		cfg.Workspace, _ = os.Getwd()
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("store %s: %v", cfg.DataDir, err)
	}
	defer st.Close()
	log.Printf("phonelab %s: data %s, workspace %s", version, cfg.DataDir, cfg.Workspace)

	m := core.NewManager(st)
	m.Root, m.Version, m.Started = cfg.Workspace, version, time.Now()
	for _, d := range cfg.Devices {
		drv, err := device.New(d.Driver, d.Opts)
		if err != nil {
			log.Fatalf("device %s: %v", d.ID, err)
		}
		var op transport.Opener = &transport.SerialOpener{Cfg: transport.SerialConfig{Port: d.Port, VID: d.VID, PID: d.PID}}
		if po, ok := drv.(interface{ Opener() transport.Opener }); ok && po.Opener() != nil {
			op = po.Opener() // e.g. the nokia driver over TCP
		}
		if mf := firstNonEmpty(*mockFile, d.Mock); mf != "" {
			lines, err := mock.LoadTranscript(mf, 0)
			if err != nil {
				log.Fatalf("mock %s: %v", mf, err)
			}
			ph := mock.New(lines)
			ph.LineDelay = time.Millisecond
			op = ph.Opener()
			log.Printf("device %s: MOCK phone replaying %s", d.ID, mf)
		}
		if err := m.Add(core.NewSession(d.ID, drv, op, st, m.Bus)); err != nil {
			log.Fatal(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a := &rest.Server{M: m, Token: cfg.Token}
	spa, _ := fs.Sub(webui.Dist, "dist")
	h := a.Handler(map[string]http.Handler{
		"/mcp": mcp.Handler(m, version),
		"/":    spaHandler(spa),
	})
	srv := &http.Server{Addr: cfg.Listen, Handler: h, ReadHeaderTimeout: 10 * time.Second}

	go m.Run(ctx)
	if cfg.RetentionDays > 0 {
		go func() {
			for {
				if n, err := st.Prune(time.Duration(cfg.RetentionDays) * 24 * time.Hour); err != nil || n > 0 {
					log.Printf("retention: pruned %d boots (%v)", n, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Hour):
				}
			}
		}()
	}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sc)
	}()
	log.Printf("listening on http://%s  (API /api, MCP /mcp, UI /)", cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let the line writers flush
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// spaHandler serves the built web UI, falling back to index.html for client-side routes.
func spaHandler(root fs.FS) http.Handler {
	fsrv := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path[1:]
		if p != "" {
			if _, err := fs.Stat(root, p); err == nil {
				fsrv.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "web UI not built (cd web && npm run build)", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}
