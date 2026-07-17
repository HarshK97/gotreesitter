package parsercorephase0

import (
	"errors"
	"math"
	"math/bits"
)

const (
	boundaryIndexInitialCapacity = 16
	boundaryIndexLoadNumerator   = 7
	boundaryIndexLoadDenominator = 10
)

var errBoundaryIndexCapacity = errors.New("parser-core phase zero: canonical boundary index capacity")

// boundaryIndex is an exact, retained, frontier-local hash table. Slots from
// earlier frontiers remain allocated but become empty in O(1) by advancing the
// generation. Lookup always confirms the complete boundaryKey after hashing.
type boundaryIndex struct {
	slots      []boundarySlot
	generation uint32
	count      uint32
	maxSlots   uint32
}

type boundarySlot struct {
	key        boundaryKey
	id         NodeID
	generation uint32
}

type boundaryIndexSnapshot struct {
	slots      []boundarySlot
	generation uint32
	count      uint32
}

// boundaryMutation names the backing array as well as the changed slot. A
// transaction may grow the index, so rollback must be able to restore writes
// made both before and after the rehash.
type boundaryMutation struct {
	slots    []boundarySlot
	index    uint32
	previous boundarySlot
}

func newBoundaryIndex(maxEntries uint32) (boundaryIndex, error) {
	if maxEntries == 0 {
		return boundaryIndex{}, errBoundaryIndexCapacity
	}
	required := (uint64(maxEntries)*boundaryIndexLoadDenominator + boundaryIndexLoadNumerator - 1) / boundaryIndexLoadNumerator
	if required < boundaryIndexInitialCapacity {
		required = boundaryIndexInitialCapacity
	}
	if required > uint64(math.MaxInt) {
		return boundaryIndex{}, errBoundaryIndexCapacity
	}
	maxSlots := uint64(1) << bits.Len64(required-1)
	if maxSlots > math.MaxUint32 || maxSlots > uint64(math.MaxInt) {
		return boundaryIndex{}, errBoundaryIndexCapacity
	}
	return boundaryIndex{generation: 1, maxSlots: uint32(maxSlots)}, nil
}

func (i *boundaryIndex) snapshot() boundaryIndexSnapshot {
	return boundaryIndexSnapshot{slots: i.slots, generation: i.generation, count: i.count}
}

func (i *boundaryIndex) restore(snapshot boundaryIndexSnapshot) {
	i.slots = snapshot.slots
	i.generation = snapshot.generation
	i.count = snapshot.count
}

func (i *boundaryIndex) get(key boundaryKey) (NodeID, bool) {
	if len(i.slots) == 0 {
		return 0, false
	}
	mask := uint64(len(i.slots) - 1)
	start := boundaryKeyHash(key) & mask
	for probe := uint64(0); probe < uint64(len(i.slots)); probe++ {
		slot := &i.slots[(start+probe)&mask]
		if slot.generation != i.generation {
			return 0, false
		}
		if slot.key == key {
			return slot.id, true
		}
	}
	return 0, false
}

func (i *boundaryIndex) set(key boundaryKey, id NodeID, journal *[]boundaryMutation, transactional bool) error {
	if id == 0 {
		return errors.New("parser-core phase zero: zero canonical boundary node")
	}
	if len(i.slots) == 0 {
		if err := i.grow(boundaryIndexInitialCapacity); err != nil {
			return err
		}
	}
	index, found := i.find(key)
	if !found && uint64(i.count+1)*boundaryIndexLoadDenominator > uint64(len(i.slots))*boundaryIndexLoadNumerator {
		if len(i.slots) >= int(i.maxSlots) {
			return errBoundaryIndexCapacity
		}
		if err := i.grow(len(i.slots) * 2); err != nil {
			return err
		}
		index, found = i.find(key)
	}
	if transactional {
		*journal = append(*journal, boundaryMutation{
			slots: i.slots, index: index, previous: i.slots[index],
		})
	}
	i.slots[index] = boundarySlot{key: key, id: id, generation: i.generation}
	if !found {
		i.count++
	}
	return nil
}

func (i *boundaryIndex) find(key boundaryKey) (uint32, bool) {
	mask := uint64(len(i.slots) - 1)
	start := boundaryKeyHash(key) & mask
	for probe := uint64(0); probe < uint64(len(i.slots)); probe++ {
		index := uint32((start + probe) & mask)
		slot := &i.slots[index]
		if slot.generation != i.generation {
			return index, false
		}
		if slot.key == key {
			return index, true
		}
	}
	return 0, false
}

func (i *boundaryIndex) grow(capacity int) error {
	if capacity < boundaryIndexInitialCapacity {
		capacity = boundaryIndexInitialCapacity
	}
	if capacity > int(i.maxSlots) || capacity <= 0 || capacity&(capacity-1) != 0 {
		return errBoundaryIndexCapacity
	}
	previous := i.slots
	i.slots = make([]boundarySlot, capacity)
	for index := range previous {
		slot := previous[index]
		if slot.generation != i.generation {
			continue
		}
		destination, found := i.find(slot.key)
		if found {
			return errors.New("parser-core phase zero: duplicate canonical boundary during rehash")
		}
		i.slots[destination] = slot
	}
	return nil
}

func (i *boundaryIndex) advanceGeneration() {
	if i.generation == math.MaxUint32 {
		clear(i.slots)
		i.generation = 1
	} else {
		i.generation++
	}
	i.count = 0
}

func (i *boundaryIndex) reset() {
	i.advanceGeneration()
}

func (i *boundaryIndex) logicalMap() map[boundaryKey]NodeID {
	result := make(map[boundaryKey]NodeID, i.count)
	for index := range i.slots {
		slot := i.slots[index]
		if slot.generation == i.generation {
			result[slot.key] = slot.id
		}
	}
	return result
}

func boundaryKeyHash(key boundaryKey) uint64 {
	// CheckpointID already names exact serialized scanner bytes. Complete key
	// equality still resolves every table-hash collision.
	h := key.frontier * 0x9e3779b97f4a7c15
	h ^= (uint64(key.state) << 32) | uint64(key.byteOffset)
	h ^= uint64(key.checkpoint) * 0x94d049bb133111eb
	if key.shifted {
		h ^= 0xd6e8feb86659fd93
	}
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	return h ^ (h >> 31)
}
