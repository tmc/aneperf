//go:build darwin

// Command aneperfweb serves a web dashboard for Apple Neural Engine metrics.
//
// It streams ANE telemetry over Server-Sent Events to a browser-based
// dashboard. A single Sampler instance is shared across all connected clients.
//
// Usage:
//
//	aneperfweb [flags]
//
// Flags:
//
//	--addr      listen address (default :9092)
//	--interval  sample interval (default 1s)
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/tmc/aneperf"
)

//go:embed web/index.html
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":9092", "listen address")
	interval := flag.Duration("interval", 1*time.Second, "sample interval")
	flag.Parse()

	if err := run(*addr, *interval); err != nil {
		fmt.Fprintf(os.Stderr, "aneperfweb: %v\n", err)
		os.Exit(1)
	}
}

func run(addr string, interval time.Duration) error {
	sampler, err := aneperf.NewSampler()
	if err != nil {
		return err
	}
	defer sampler.Close()

	hub := newHub(sampler, interval)
	go hub.run()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /api/stream", hub.handleStream)
	mux.HandleFunc("GET /api/device", handleDevice)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-sig
		log.Println("shutting down")
		hub.stop()
		server.Close()
	}()

	log.Printf("listening on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleDevice(w http.ResponseWriter, r *http.Request) {
	info, err := aneperf.ReadDeviceInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// hub fans out sampler deltas to all connected SSE clients.
type hub struct {
	sampler  *aneperf.Sampler
	interval time.Duration

	mu      sync.Mutex
	clients map[chan []byte]struct{}
	done    chan struct{}
}

func newHub(s *aneperf.Sampler, interval time.Duration) *hub {
	return &hub{
		sampler:  s,
		interval: interval,
		clients:  make(map[chan []byte]struct{}),
		done:     make(chan struct{}),
	}
}

func (h *hub) run() {
	snap := h.sampler.Start()
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
		}

		delta := h.sampler.Stop(snap)
		snap = h.sampler.Start()

		data, err := json.Marshal(delta)
		if err != nil {
			continue
		}

		h.mu.Lock()
		for ch := range h.clients {
			select {
			case ch <- data:
			default:
				// slow client, drop
			}
		}
		h.mu.Unlock()
	}
}

func (h *hub) stop() {
	close(h.done)
}

func (h *hub) addClient() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) removeClient(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *hub) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.addClient()
	defer h.removeClient(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-ch:
			fmt.Fprintf(w, "event: delta\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
