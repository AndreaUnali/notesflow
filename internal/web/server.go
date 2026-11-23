// Package web serves the embedded graph and its live snapshots over loopback.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
)

//go:embed assets/*
var embedded embed.FS

type Server struct {
	http     *http.Server
	listener net.Listener
	URL      string
}

func Listen(ctx context.Context, port int, store *graph.Store, edit func(context.Context, parser.Node) error) (*Server, error) {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	host := listener.Addr().String()
	origin := "http://" + host
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /api/graph", func(w http.ResponseWriter, r *http.Request) {
		snapshot, _ := store.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		stream(w, r, store)
	})
	mux.HandleFunc("POST /api/edit", func(w http.ResponseWriter, r *http.Request) {
		contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Header.Get("Origin") != origin || contentType != "application/json" {
			http.Error(w, "Origine o tipo di richiesta non valido", http.StatusForbidden)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || body.ID == "" {
			http.Error(w, "ID non valido", http.StatusBadRequest)
			return
		}
		snapshot, _ := store.Snapshot()
		node, found := graph.SourceNode(snapshot, body.ID)
		if found {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if err := edit(ctx, node); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "Nodo non trovato", http.StatusNotFound)
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			http.Error(w, "Accesso locale richiesto", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
	return &Server{
		http: &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second,
			BaseContext: func(net.Listener) context.Context { return ctx }},
		listener: listener, URL: origin,
	}, nil
}

func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func stream(w http.ResponseWriter, r *http.Request, store *graph.Store) {
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var revision uint64
	for {
		snapshot, changed := store.Snapshot()
		if revision != snapshot.Revision {
			payload, err := json.Marshal(snapshot)
			if err != nil || writeEvent(controller, w, "event: graph\ndata: %s\n\n", payload) != nil {
				return
			}
			revision = snapshot.Revision
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-ticker.C:
			if writeEvent(controller, w, ": heartbeat\n\n", nil) != nil {
				return
			}
		}
	}
}

func writeEvent(controller *http.ResponseController, w io.Writer, format string, data []byte) error {
	if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	defer controller.SetWriteDeadline(time.Time{})
	var err error
	if data == nil {
		_, err = io.WriteString(w, format)
	} else {
		_, err = fmt.Fprintf(w, format, data)
	}
	if err != nil {
		return err
	}
	return controller.Flush()
}
