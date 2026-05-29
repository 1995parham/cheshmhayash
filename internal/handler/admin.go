package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

type admin struct {
	mgr *natsx.Manager
	log *slog.Logger
}

func (a *admin) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/clusters", a.listClusters)
	mux.HandleFunc("GET /api/admin/clusters/{cluster}/servers", a.ping)
	mux.HandleFunc("GET /api/admin/clusters/{cluster}/servers/{endpoint}", a.pingEndpoint)
	mux.HandleFunc("GET /api/admin/clusters/{cluster}/servers/{id}/{endpoint}", a.serverEndpoint)
	mux.HandleFunc("GET /api/admin/clusters/{cluster}/accounts/{account}/{endpoint}", a.accountEndpoint)
	mux.HandleFunc("POST /api/admin/clusters/{cluster}/servers/{id}/actions/reload", a.reload)
	mux.HandleFunc("POST /api/admin/clusters/{cluster}/servers/{id}/actions/lame-duck", a.lameDuck)
	mux.HandleFunc("POST /api/admin/clusters/{cluster}/servers/{id}/actions/kick", a.kick)
}

func (a *admin) resolve(w http.ResponseWriter, r *http.Request) *natsx.Cluster {
	name := r.PathValue("cluster")
	c := a.mgr.Get(name)
	if c == nil {
		writeJSON(w, http.StatusNotFound, apiError{Message: "cluster '" + name + "' not configured"})
		return nil
	}
	return c
}

func (a *admin) listClusters(w http.ResponseWriter, _ *http.Request) {
	names := a.mgr.Names()
	sort.Strings(names)
	writeJSON(w, http.StatusOK, names)
}

func (a *admin) ping(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.Ping()
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRawArray(w, out)
}

func (a *admin) pingEndpoint(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	ep := r.PathValue("endpoint")
	if !natsx.IsServerEndpoint(ep) {
		unknownEndpoint(w, ep, natsx.ServerEndpoints)
		return
	}
	out, err := c.PingEndpoint(ep)
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRawArray(w, out)
}

func (a *admin) serverEndpoint(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	ep := r.PathValue("endpoint")
	if !natsx.IsServerEndpoint(ep) {
		unknownEndpoint(w, ep, natsx.ServerEndpoints)
		return
	}
	out, err := c.ServerEndpoint(r.PathValue("id"), ep)
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRaw(w, out)
}

func (a *admin) accountEndpoint(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	ep := r.PathValue("endpoint")
	if !natsx.IsAccountEndpoint(ep) {
		unknownEndpoint(w, ep, natsx.AccountEndpoints)
		return
	}
	out, err := c.AccountEndpoint(r.PathValue("account"), ep)
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRawArray(w, out)
}

func (a *admin) reload(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.Reload(r.PathValue("id"))
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRaw(w, out)
}

func (a *admin) lameDuck(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	out, err := c.LameDuck(r.PathValue("id"))
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRaw(w, out)
}

type kickBody struct {
	CID uint64 `json:"cid"`
}

func (a *admin) kick(w http.ResponseWriter, r *http.Request) {
	c := a.resolve(w, r)
	if c == nil {
		return
	}
	var body kickBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "decode body: " + err.Error()})
		return
	}
	if body.CID == 0 {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "cid is required"})
		return
	}
	out, err := c.Kick(r.PathValue("id"), body.CID)
	if err != nil {
		upstreamError(w, a.log, err)
		return
	}
	writeRaw(w, out)
}

func unknownEndpoint(w http.ResponseWriter, ep string, allowed []string) {
	msg := "unknown endpoint '" + ep + "'; valid: " + strings.Join(allowed, ", ")
	writeJSON(w, http.StatusBadRequest, apiError{Message: msg})
}
