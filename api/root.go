package api

import (
	"errors"
	"net/http"

	util "github.com/qri-io/apiutil"
	"github.com/qri-io/qri/config"
	"github.com/qri-io/qri/lib"
)

// RootHandler bundles handlers that may need to be called
// by "/"
// TODO - This will be removed when we add a proper router
type RootHandler struct {
	dsh *DatasetHandlers
	ph  *PeerHandlers
}

// NewRootHandler creates a new RootHandler
func NewRootHandler(dsh *DatasetHandlers, ph *PeerHandlers) *RootHandler {
	return &RootHandler{dsh, ph}
}

// Handler is the only Handler func for the RootHandler struct
func (mh *RootHandler) Handler(w http.ResponseWriter, r *http.Request) {
	ref := DatasetRefFromCtx(r.Context())
	if ref.IsEmpty() {
		HealthCheckHandler(w, r)
		return
	}

	if ref.IsPeerRef() {
		p := &lib.PeerInfoParams{
			Peername: ref.Peername,
		}
		res := &config.ProfilePod{}
		err := mh.ph.Info(p, res)
		if err != nil {
			util.WriteErrResponse(w, http.StatusInternalServerError, err)
			return
		}
		if res.ID == "" {
			util.WriteErrResponse(w, http.StatusNotFound, errors.New("cannot find peer"))
			return
		}
		util.WriteResponse(w, res)
		return
	}

	p := lib.GetParams{
		Path:   ref.String(),
		Filter: r.FormValue("filter"),
	}
	res := lib.GetResult{}
	err := mh.dsh.Get(&p, &res)
	if err != nil {
		util.WriteErrResponse(w, http.StatusInternalServerError, err)
		return
	}

	if res.Source == nil || res.Source.IsEmpty() {
		util.WriteErrResponse(w, http.StatusNotFound, errors.New("cannot find peer dataset"))
		return
	}

	// ref = repo.DatasetRef{
	// 	Peername:  res.Dataset.Peername,
	// 	ProfileID: profile.ID(res.Dataset.ProfileID),
	// 	Name:      res.Dataset.Name,
	// 	Path:      res.Dataset.Path,
	// 	Dataset:   res.Dataset,
	// }

	util.WriteResponse(w, res.Result)
	return
}
