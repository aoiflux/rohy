package graphene

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/store"
)

// declare makes the declarations rohy's schema relies on, at every open: the names of its two
// custom types, and the timestamp key ordered. A read-only open writes nothing, so it registers the
// names in this process and declares nothing; it answers ranges correctly without the ordering,
// only by a slower plan, and a case opened writable since v0.3.0 carries the declaration anyway.
func declare(g *graphene.Graph, dir string, readOnly bool) error {
	if err := nameTypes(g, dir, readOnly); err != nil {
		return err
	}
	if readOnly {
		return nil
	}
	return declareOrdered(g)
}

// declareOrdered declares the timestamp key ordered, so a time range is driven by a binary search of
// the key rather than by every event's label with the range as a residual (PERFORMANCE.md §9).
//
// The declaration is the engine's to keep: graphene.schema names it and the image carries the
// ordering (GORD), so an open of a case that has it pays nothing for it. Declaring is skipped when
// it is in force, because it rewrites graphene.schema durably. On a case written before v0.3.0 it
// is made once, and the next seal writes the ordering into the image.
//
// The timestamp's index value is fixed-width UTC (timestampIndex), so the byte order the ordered
// key compares by is chronological order.
func declareOrdered(g *graphene.Graph) error {
	if keys, _ := g.OrderedProperties(); slices.Contains(keys, consts.PropTimestamp) {
		return nil
	}
	if err := g.DeclareOrderedNodeProperty(consts.PropTimestamp); err != nil {
		return fmt.Errorf("declare the timestamp ordered: %w", err)
	}
	return nil
}

// nameTypes names rohy's two custom types for the engine, so its errors and tools say Event and
// Relation rather than Custom(0), and a case reads with names in graphene's own command line.
//
// The names are registered in this process on every open: an open has already registered what the
// case's labels file holds, so a file that names either number differently is refused here rather
// than rendered wrongly. A writable open then records them beside the image when the file does not
// name them yet. Recording rewrites the file durably, so it is not repeated on every open, and a
// read-only open writes nothing.
func nameTypes(g *graphene.Graph, dir string, readOnly bool) error {
	if err := store.RegisterNodeTypeName(consts.NodeEvent, consts.NodeEventName); err != nil {
		return fmt.Errorf("name the event type: %w", err)
	}
	if err := store.RegisterEdgeTypeName(consts.EdgeRelation, consts.EdgeRelationName); err != nil {
		return fmt.Errorf("name the relation type: %w", err)
	}
	if readOnly || dir == "" {
		return nil
	}
	if named, err := labelsNameRohysTypes(dir); err != nil || named {
		return err
	}
	return g.DeclareTypeNames(
		map[store.NodeType]string{consts.NodeEvent: consts.NodeEventName},
		map[store.EdgeType]string{consts.EdgeRelation: consts.EdgeRelationName},
	)
}

// labelsNameRohysTypes reports whether the case's labels file already names both types as rohy
// does. A line naming either differently never gets here: the open registered it, and nameTypes's
// registration conflicted with it.
func labelsNameRohysTypes(dir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, consts.TypeNamesFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, line := range []string{
		fmt.Sprintf("node\t%d\t%s\n", uint16(consts.NodeEvent), consts.NodeEventName),
		fmt.Sprintf("edge\t%d\t%s\n", uint16(consts.EdgeRelation), consts.EdgeRelationName),
	} {
		if !bytes.Contains(data, []byte(line)) {
			return false, nil
		}
	}
	return true, nil
}
