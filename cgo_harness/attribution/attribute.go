package main

import "sort"

// attributionResult is one profile's fully-mapped receipt row: total sampled
// CPU nanoseconds, the per-component share of that total, and every
// unclassified function the walk had to fall through to ComponentOther for
// (a nonzero, non-runtime entry here is a signal the classify.go table has a
// gap and should grow).
type attributionResult struct {
	TotalSampleNS   int64
	ComponentNS     map[Component]int64
	UnclassifiedTop []unclassifiedEntry // largest-first, runtime/GC excluded
}

type unclassifiedEntry struct {
	Function string
	NS       int64
}

// attributeProfile walks every sample's call stack leaf-to-root, assigning
// the sample's full value to the first frame classify.go recognizes. Frames
// classify.go marks as shared/transparent (or does not recognize at all) are
// skipped over; the walk continues toward the root. A sample with no
// recognized frame anywhere on its stack is ComponentOther.
func attributeProfile(profile *pbProfile) attributionResult {
	result := attributionResult{ComponentNS: map[Component]int64{}}
	unclassified := map[string]int64{}

	for _, sample := range profile.Samples {
		result.TotalSampleNS += sample.ValueNS
		component := ComponentOther
		matched := false
		for _, frame := range sample.Stack {
			if c, ok := classifyFunction(frame); ok {
				component = c
				matched = true
				break
			}
		}
		result.ComponentNS[component] += sample.ValueNS
		if !matched {
			leaf := "<empty stack>"
			if len(sample.Stack) > 0 {
				leaf = sample.Stack[0].Function
			}
			if !isRuntimeFrame(leaf) {
				unclassified[leaf] += sample.ValueNS
			}
		}
	}

	for fn, ns := range unclassified {
		result.UnclassifiedTop = append(result.UnclassifiedTop, unclassifiedEntry{Function: fn, NS: ns})
	}
	sort.Slice(result.UnclassifiedTop, func(i, j int) bool {
		return result.UnclassifiedTop[i].NS > result.UnclassifiedTop[j].NS
	})
	return result
}

// isRuntimeFrame reports whether a leaf function name is Go runtime/GC/
// scheduler bookkeeping with no gotreesitter-domain meaning -- the expected,
// legitimate content of ComponentOther, not a classification gap.
func isRuntimeFrame(name string) bool {
	prefixes := []string{"runtime.", "gc", "sync.", "sync/atomic."}
	for _, p := range prefixes {
		if len(name) >= len(p) && name[:len(p)] == p {
			return true
		}
	}
	return false
}
