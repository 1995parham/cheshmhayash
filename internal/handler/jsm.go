package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

type jsm struct {
	mgr   *natsx.Manager
	cache *natsx.OverviewCache // optional; when nil, /overview hits NATS directly
	log   *slog.Logger
}

func (j *jsm) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/overview", j.overview)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/overview/stream", j.overviewStream)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams", j.listStreams)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams/{stream}", j.streamInfo)
	mux.HandleFunc("PUT /api/jsm/clusters/{cluster}/streams/{stream}", j.updateStream)
	mux.HandleFunc("DELETE /api/jsm/clusters/{cluster}/streams/{stream}", j.deleteStream)
	mux.HandleFunc("POST /api/jsm/clusters/{cluster}/streams/{stream}/purge", j.purgeStream)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams/{stream}/consumers", j.listConsumers)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams/{stream}/consumers/{consumer}", j.consumerInfo)
	mux.HandleFunc("DELETE /api/jsm/clusters/{cluster}/streams/{stream}/consumers/{consumer}", j.deleteConsumer)
	mux.HandleFunc("POST /api/jsm/clusters/{cluster}/actions/meta-stepdown", j.metaStepdown)
	mux.HandleFunc("POST /api/jsm/clusters/{cluster}/streams/{stream}/actions/stepdown", j.streamStepdown)
	mux.HandleFunc("POST /api/jsm/clusters/{cluster}/streams/{stream}/consumers/{consumer}/actions/stepdown", j.consumerStepdown)
}

func (j *jsm) resolve(w http.ResponseWriter, r *http.Request) *natsx.Cluster {
	name := r.PathValue("cluster")
	c := j.mgr.Get(name)
	if c == nil {
		writeJSON(w, http.StatusNotFound, apiError{Message: "cluster '" + name + "' not configured"})
		return nil
	}
	return c
}

func parseOffset(r *http.Request) uint64 {
	v := r.URL.Query().Get("offset")
	if v == "" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func requireConfirm(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("confirm") != "true" {
		writeJSON(w, http.StatusPreconditionRequired, apiError{
			Message: "destructive action requires ?confirm=true",
		})
		return false
	}
	return true
}

// overview serves the latest JSZ overview. By default reads from the
// background cache (refreshed every natsx.DefaultOverviewPeriod). Pass
// ?live=true to force a NATS round-trip — useful when the operator wants
// truth at "now" and is OK paying the cost.
func (j *jsm) overview(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	live := r.URL.Query().Get("live") == "true"
	if j.cache != nil && !live {
		if snap, ok := j.cache.Get(c.Name()); ok {
			if snap.Err != "" {
				writeJSON(w, http.StatusBadGateway, apiError{Message: snap.Err})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Overview-Age-Seconds", fmt.Sprintf("%.1f", time.Since(snap.Updated).Seconds()))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(snap.Data)
			return
		}
		// Cache miss (cold start) — fall through to live fetch.
	}
	out, err := c.JSMOverview()
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRawArray(w, out)
}

// overviewStream is an SSE endpoint that pushes a fresh overview on
// every cache refresh tick. Each message body is the same JSON array
// /overview returns; on refresh error a single `event: error` is sent.
// EventSource on the client side reconnects automatically.
func (j *jsm) overviewStream(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	if j.cache == nil {
		http.Error(w, "overview cache disabled", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx
	w.WriteHeader(http.StatusOK)

	ch := j.cache.Subscribe(c.Name())
	defer j.cache.Unsubscribe(c.Name(), ch)

	ctx := r.Context()
	// Heartbeat so proxies don't drop an idle connection between refreshes.
	hb := time.NewTicker(20 * time.Second)
	defer hb.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hb.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case snap, ok := <-ch:
			if !ok {
				return
			}
			if snap.Err != "" {
				if _, err := fmt.Fprintf(w, "event: error\ndata: %q\n\n", snap.Err); err != nil {
					return
				}
			} else {
				// SSE data lines must not contain raw newlines. The
				// overview is a compact JSON array (one line), so a
				// single `data:` line is correct.
				if _, err := fmt.Fprintf(w, "data: %s\n\n", snap.Data); err != nil {
					return
				}
			}
			flusher.Flush()
		}
	}
}

func (j *jsm) listStreams(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.ListStreams(parseOffset(r))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) streamInfo(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.StreamInfo(r.PathValue("stream"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) updateStream(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	stream := r.PathValue("stream")

	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{
			Message: "body must be a StreamConfig JSON object: " + err.Error(),
		})
		return
	}
	// Guard against silently renaming or targeting the wrong stream.
	if rawName, ok := body["name"]; ok {
		var n string
		if err := json.Unmarshal(rawName, &n); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "config.name must be a string"})
			return
		}
		if n != stream {
			writeJSON(w, http.StatusBadRequest, apiError{
				Message: "config name '" + n + "' does not match URL stream '" + stream + "'",
			})
			return
		}
	} else {
		// stream is a plain identifier; a quoted string is valid JSON, so we
		// skip json.Marshal (which can't fail here anyway).
		body["name"] = json.RawMessage(strconv.Quote(stream))
	}

	payload, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "re-encode config: " + err.Error()})
		return
	}
	out, err := c.UpdateStream(stream, payload)
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) purgeStream(w http.ResponseWriter, r *http.Request) {
	if !requireConfirm(w, r) {
		return
	}
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.PurgeStream(r.PathValue("stream"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) deleteStream(w http.ResponseWriter, r *http.Request) {
	if !requireConfirm(w, r) {
		return
	}
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.DeleteStream(r.PathValue("stream"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) listConsumers(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.ListConsumers(r.PathValue("stream"), parseOffset(r))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) consumerInfo(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.ConsumerInfo(r.PathValue("stream"), r.PathValue("consumer"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) metaStepdown(w http.ResponseWriter, r *http.Request) {
	if !requireConfirm(w, r) {
		return
	}
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.MetaLeaderStepdown()
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) streamStepdown(w http.ResponseWriter, r *http.Request) {
	if !requireConfirm(w, r) {
		return
	}
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.StreamLeaderStepdown(r.PathValue("stream"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) consumerStepdown(w http.ResponseWriter, r *http.Request) {
	if !requireConfirm(w, r) {
		return
	}
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.ConsumerLeaderStepdown(r.PathValue("stream"), r.PathValue("consumer"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}

func (j *jsm) deleteConsumer(w http.ResponseWriter, r *http.Request) {
	if !requireConfirm(w, r) {
		return
	}
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.DeleteConsumer(r.PathValue("stream"), r.PathValue("consumer"))
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRaw(w, out)
}
