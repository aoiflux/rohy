package eventproof

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// graphene's node leaf, mirrored.
//
// graphene builds a node's Merkle leaf (disk/snapshot.go, nodeLeafDataV2; its spec §11.2) as
//
//	0x05 ‖ id u64le ‖ nlabels u16le ‖ label u16le × nlabels ‖ SHA256(0x07 ‖ properties)
//
// and exports no way to read one back. rohy needs two things from it — the labels and the property
// hash — to tie a verified proof to the record it is about, so the layout is copied here.
//
// 🔒 The copy is kept honest by TestLeafLayoutMatchesGraphene, which builds proofs from a real
// sealed store: if graphene ever changes the layout, that test fails before a proof is wrongly
// accepted or refused. Nothing here is consulted until graphene has verified the proof against the
// caller's root, so this code reads bytes the root already covers.
const (
	// leafTagNodeInline is the leaf of an image sealed before body v3, which hashed the property
	// blob inline. Recognised only to say what to do about it.
	leafTagNodeInline = 0x01
	leafTagNode       = 0x05
	leafTagProperties = 0x07

	leafHeaderSize = 1 + 8 + 2 // tag, id, label count
	leafLabelSize  = 2

	// snapshotBodyVersion is the one snapshot body version whose leaves this layout describes.
	//
	// 🔒 Pinned exactly, not as a minimum. graphene v0.4.0 binds body versions 2 and 3 into the
	// same root and checks only that a v3 leaf claims "3 or later", so the version byte of a proof
	// can be rewritten to anything from 3 to 255 and it still verifies. That changes no claim about
	// content today, but a proof whose stated format can be edited freely is not what a recipient
	// is told they hold — and a later version would mean a different layout this copy cannot read.
	// TestAnyByteOfTheProofMatters is what found it.
	snapshotBodyVersion = 3
)

// nodeLeaf is what a v3 node leaf says.
type nodeLeaf struct {
	id             uint64
	labels         []store.NodeType
	propertiesHash [sha256.Size]byte
}

var (
	// ErrImageOutdated: the image was sealed before leaves hashed the stored record separately.
	ErrImageOutdated = errors.New(consts.MsgEventProofLeafOld)
	errLeafShape     = errors.New(consts.MsgEventProofLeafShape)
	errBodyVersion   = errors.New(consts.MsgEventProofBodyVersion)
)

// parseNodeLeaf reads a v3 node leaf, exactly: a leaf one byte longer or shorter than its label
// count implies is refused rather than read around.
func parseNodeLeaf(leaf []byte) (nodeLeaf, error) {
	if len(leaf) > 0 && leaf[0] == leafTagNodeInline {
		return nodeLeaf{}, ErrImageOutdated
	}
	if len(leaf) < leafHeaderSize+sha256.Size || leaf[0] != leafTagNode {
		return nodeLeaf{}, errLeafShape
	}
	n := int(binary.LittleEndian.Uint16(leaf[9:leafHeaderSize]))
	if len(leaf) != leafHeaderSize+n*leafLabelSize+sha256.Size {
		return nodeLeaf{}, errLeafShape
	}
	out := nodeLeaf{
		id:     binary.LittleEndian.Uint64(leaf[1:9]),
		labels: make([]store.NodeType, n),
	}
	for i := range out.labels {
		off := leafHeaderSize + i*leafLabelSize
		out.labels[i] = store.NodeType(binary.LittleEndian.Uint16(leaf[off : off+leafLabelSize]))
	}
	copy(out.propertiesHash[:], leaf[len(leaf)-sha256.Size:])
	return out, nil
}

func (l nodeLeaf) hasLabel(t store.NodeType) bool { return slices.Contains(l.labels, t) }

// propertiesHash is graphene's separated hash of a property blob: SHA256(0x07 ‖ properties).
func propertiesHash(properties []byte) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte{leafTagProperties})
	h.Write(properties)
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}
