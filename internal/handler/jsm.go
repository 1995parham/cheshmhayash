package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

type jsm struct {
	mgr *natsx.Manager
	log *slog.Logger
}

func (j *jsm) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/overview", j.overview)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams", j.listStreams)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams/{stream}", j.streamInfo)
	mux.HandleFunc("PUT /api/jsm/clusters/{cluster}/streams/{stream}", j.updateStream)
	mux.HandleFunc("DELETE /api/jsm/clusters/{cluster}/streams/{stream}", j.deleteStream)
	mux.HandleFunc("POST /api/jsm/clusters/{cluster}/streams/{stream}/purge", j.purgeStream)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams/{stream}/consumers", j.listConsumers)
	mux.HandleFunc("GET /api/jsm/clusters/{cluster}/streams/{stream}/consumers/{consumer}", j.consumerInfo)
	mux.HandleFunc("DELETE /api/jsm/clusters/{cluster}/streams/{stream}/consumers/{consumer}", j.deleteConsumer)
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

func (j *jsm) overview(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.JSMOverview()
	if err != nil {
		upstreamError(w, j.log, err)
		return
	}
	writeRawArray(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
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
		body["name"], _ = json.Marshal(stream)
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
	writeRaw(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
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
	writeRaw(w, http.StatusOK, out)
}
