package parsercorephase0

import "unsafe"

// Byte footprints of the compact core's growable record families, computed
// once from each record type's real in-memory layout so StorageBytes stays
// authoritative if a record ever gains or loses a field.
var (
	coreNodeRecordBytes    = uint64(unsafe.Sizeof(nodeRecord{}))
	coreLinkRecordBytes    = uint64(unsafe.Sizeof(linkRecord{}))
	coreSubtreeRecordBytes = uint64(unsafe.Sizeof(subtreeRecord{}))
	coreChildRecordBytes   = uint64(unsafe.Sizeof(SubtreeID(0)))
	coreFieldRecordBytes   = uint64(unsafe.Sizeof(FieldMapEntry{}))
	coreAliasRecordBytes   = uint64(unsafe.Sizeof(Symbol(0)))
)

// StorageBytes returns a cheap, deterministic, O(1) estimate of the compact
// core's current record storage in bytes: every growable record family's
// live slice length times that family's real struct size. It costs six
// multiply-adds over already-tracked slice lengths -- no allocation, no
// traversal, no I/O -- so it is safe to call from a tight scheduler poll
// (spec.campaign.v7 tranche B8).
//
// The estimate is a lower bound on process RSS attributable to this core (it
// omits per-parse scratch, the boundary index, and checkpoint interning), but
// it grows monotonically with exactly the counters every mutating Core
// operation already increments, so it is reproducible for a given input and
// Limits: same input, same Limits, same StorageBytes trajectory, on every
// run. Callers that need a budget compatible with the production engine's
// own soft arena/scratch budget (parseMemoryBudgetForParser in the root
// package) compare this value against that same threshold.
func (c *Core) StorageBytes() uint64 {
	if c == nil {
		return 0
	}
	return uint64(len(c.nodes))*coreNodeRecordBytes +
		uint64(len(c.links))*coreLinkRecordBytes +
		uint64(len(c.subtrees))*coreSubtreeRecordBytes +
		uint64(len(c.children))*coreChildRecordBytes +
		uint64(len(c.fields))*coreFieldRecordBytes +
		uint64(len(c.aliases))*coreAliasRecordBytes
}
