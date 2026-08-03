package api

import (
	"time"

	"rohy/backend/annotate"
	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// AnnotateAPI is the Wails binding for annotation layers.
//
// A ninth bound struct rather than more methods on GraphAPI, for the reason MaintenanceAPI and
// SnapshotAPI are separate: it owns its own store and its own lifecycle, and GraphAPI is already
// the largest binding in the app.
type AnnotateAPI struct {
	store    *graphene.Store
	notes    *annotate.Store
	registry graphRegistry
}

// NewAnnotateAPI constructs the binding.
func NewAnnotateAPI(store *graphene.Store, notes *annotate.Store, registry graphRegistry) *AnnotateAPI {
	return &AnnotateAPI{store: store, notes: notes, registry: registry}
}

func (a *AnnotateAPI) graphID(requested uint64) uint64 {
	if requested != 0 {
		return requested
	}
	if a.registry != nil {
		if id := a.registry.Active(); id != 0 {
			return id
		}
	}
	return consts.DefaultGraphID
}

// AnnotationView is a graph's annotations plus what the canvas needs to place them.
type AnnotationView struct {
	Document *annotate.Document `json:"document"`
	// NodeOf maps each anchored event hash to the node id currently holding it, so the canvas can
	// place a pin without resolving hashes itself.
	//
	// 🔒 Resolved fresh on every read rather than stored: the whole reason annotations are
	// hash-anchored is that node ids move. A cached mapping would reintroduce exactly the staleness
	// the hash exists to avoid.
	NodeOf map[string]uint64 `json:"node_of"`
	// Orphaned lists anchored hashes that no event in the case carries any more. Reported rather
	// than dropped: a note that quietly stops being drawn looks like a note that was never made.
	Orphaned []string `json:"orphaned"`
}

// Annotations returns one graph's annotations, with anchors resolved against the case as it is.
func (a *AnnotateAPI) Annotations(graphID uint64) (AnnotationView, error) {
	var out AnnotationView
	if a.notes == nil {
		return out, nil
	}
	gid := a.graphID(graphID)
	doc, err := a.notes.Get(gid)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	out.Document = doc

	wanted, err := a.notes.Hashes(gid)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	out.NodeOf = map[string]uint64{}
	out.Orphaned = []string{}
	if len(wanted) == 0 {
		return out, nil
	}

	hashes, err := a.store.EventHashes()
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	byHash := make(map[string]uint64, len(hashes))
	for id, h := range hashes {
		if h == "" {
			continue
		}
		// Lowest id wins, so the mapping does not depend on map order.
		if cur, seen := byHash[h]; !seen || id < cur {
			byHash[h] = id
		}
	}
	for _, h := range wanted {
		if id, ok := byHash[h]; ok {
			out.NodeOf[h] = id
		} else {
			out.Orphaned = append(out.Orphaned, h)
		}
	}
	return out, nil
}

// LayerRequest adds or updates a layer. An empty ID adds.
type LayerRequest struct {
	GraphID uint64 `json:"graph_id"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Colour  string `json:"colour,omitempty"`
	Visible bool   `json:"visible"`
	Z       int    `json:"z"`
}

// SaveLayer adds a layer, or updates the one named by ID.
func (a *AnnotateAPI) SaveLayer(req LayerRequest) (*annotate.Layer, error) {
	if a.notes == nil {
		return nil, nil
	}
	gid := a.graphID(req.GraphID)
	if req.ID == "" {
		l, err := a.notes.AddLayer(gid, req.Name, req.Colour, time.Now().UTC())
		if err != nil {
			return nil, AsError(consts.ErrCodeInternal, err)
		}
		return l, nil
	}
	l, err := a.notes.UpdateLayer(gid, annotate.Layer{
		ID: req.ID, Name: req.Name, Colour: req.Colour, Visible: req.Visible, Z: req.Z,
	})
	if err != nil {
		return nil, AsError(consts.ErrCodeInternal, err)
	}
	return l, nil
}

// LayerDeletion reports what deleting a layer would take with it, so the UI can ask before doing
// it rather than after.
type LayerDeletion struct {
	Annotations int `json:"annotations"`
}

// CountOnLayer reports how many annotations a layer holds.
func (a *AnnotateAPI) CountOnLayer(graphID uint64, layerID string) (LayerDeletion, error) {
	if a.notes == nil {
		return LayerDeletion{}, nil
	}
	n, err := a.notes.CountOnLayer(a.graphID(graphID), layerID)
	if err != nil {
		return LayerDeletion{}, AsError(consts.ErrCodePersistence, err)
	}
	return LayerDeletion{Annotations: n}, nil
}

// DeleteLayer removes a layer and everything on it, returning how many annotations went with it.
func (a *AnnotateAPI) DeleteLayer(graphID uint64, layerID string) (LayerDeletion, error) {
	if a.notes == nil {
		return LayerDeletion{}, nil
	}
	n, err := a.notes.DeleteLayer(a.graphID(graphID), layerID)
	if err != nil {
		return LayerDeletion{}, AsError(consts.ErrCodeInternal, err)
	}
	return LayerDeletion{Annotations: n}, nil
}

// AnnotationRequest creates or updates one annotation. An empty ID creates.
type AnnotationRequest struct {
	GraphID uint64        `json:"graph_id"`
	Item    annotate.Item `json:"item"`
}

// SaveAnnotation writes one annotation.
func (a *AnnotateAPI) SaveAnnotation(req AnnotationRequest) (*annotate.Item, error) {
	if a.notes == nil {
		return nil, nil
	}
	it, err := a.notes.Put(a.graphID(req.GraphID), req.Item, time.Now().UTC())
	if err != nil {
		return nil, AsError(consts.ErrCodeInternal, err)
	}
	return it, nil
}

// DeleteAnnotation removes one. Deleting one that is already gone is success.
func (a *AnnotateAPI) DeleteAnnotation(graphID uint64, id string) error {
	if a.notes == nil {
		return nil
	}
	if err := a.notes.Delete(a.graphID(graphID), id); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}
