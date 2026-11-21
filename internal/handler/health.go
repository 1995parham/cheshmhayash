package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/1995parham/cheshmhayash/internal/health"
)

// healthResponse is a health.Report with the time of the overview it read.
type healthResponse struct {
	health.Report

	Updated    time.Time `json:"updated"`
	MinIdle    string    `json:"min_idle"`
	MinPending int64     `json:"min_pending"`
}

// health runs the rules of internal/health over the cluster's overview: the
// cached snapshot, or a fresh one with ?live=true or on a cold cache.
// ?min_idle= (a Go duration) and ?min_pending= change the stale-consumer
// thresholds.
func (j *jsm) health(w http.ResponseWriter, r *http.Request) {
	c := j.resolve(w, r)
	if c == nil {
		return
	}

	opts := health.DefaultOptions()
	q := r.URL.Query()

	if v := q.Get("min_idle"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "min_idle must be a duration such as 24h"})
			return
		}
		opts.MinIdle = d
	}

	if v := q.Get("min_pending"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "min_pending must be a whole number"})
			return
		}
		opts.MinPending = n
	}

	data, updated, ok := j.cachedOverview(w, c.Name(), q.Get("live") == "true")
	if !ok {
		return
	}
	if data == nil {
		out, err := c.JSMOverview()
		if err != nil {
			upstreamError(w, j.log, err)
			return
		}
		data, updated = marshalReplies(out), time.Now()
	}

	rep, err := health.Analyze(data, time.Now(), opts)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, healthResponse{
		Report:     rep,
		Updated:    updated,
		MinIdle:    opts.MinIdle.String(),
		MinPending: opts.MinPending,
	})
}
