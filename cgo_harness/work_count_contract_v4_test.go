//go:build cgo && treesitter_c_parity && treesitter_c_perfscan

package cgoharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const workCountConvergenceCorruptionEnv = "GTS_WORK_COUNT_CONVERGENCE_CORRUPTION_CHILD"

func TestWorkCountV4SchemaBoundaries(t *testing.T) {
	if workCountGoAdmissionChildSchema != "gts-work-count-go-child/v3" ||
		workCountTaggedGoChildSchema != "gts-work-count-go-child/v4" ||
		workCountCChildSchema != "gts-work-count-c-child/v3" ||
		workCountReceiptSchema != "gts-work-count-receipt/v4" ||
		workCountContract != "gts-work-count/v2" {
		t.Fatalf("mixed work-count schema boundary: admission=%q tagged=%q C=%q receipt=%q counters=%q", workCountGoAdmissionChildSchema, workCountTaggedGoChildSchema, workCountCChildSchema, workCountReceiptSchema, workCountContract)
	}
}

func TestWorkCountConvergenceAdmissionAcceptsValidFrontier(t *testing.T) {
	frontier := workCountValidConvergenceForTest()
	workCountValidateGoConvergence(t, frontier)
	data, err := json.Marshal(frontier)
	if err != nil {
		t.Fatal(err)
	}
	workCountValidateGoConvergenceObject(t, data)
}

func TestWorkCountConvergenceAdmissionRejectsCorruption(t *testing.T) {
	if corruption := os.Getenv(workCountConvergenceCorruptionEnv); corruption != "" {
		frontier := workCountValidConvergenceForTest()
		makeSecondEventReject := func() {
			frontier.Events[1].Phase = "boundary_equivalence"
			frontier.Events[1].Outcome = "rejected"
			frontier.Events[1].RejectReason = "status"
			frontier.Aggregates.Phases.FinalSelect = 0
			frontier.Aggregates.Phases.BoundaryEquivalence = 1
			frontier.Aggregates.Outcomes.Selected = 0
			frontier.Aggregates.Outcomes.Rejected = 1
			frontier.Aggregates.Rejections.Status = 1
			frontier.FirstRejects = []workCountConvergenceEvent{frontier.Events[1]}
		}
		switch corruption {
		case "retained_short":
			frontier.Events = frontier.Events[:1]
		case "sequence_gap":
			frontier.Events[1].Sequence = 3
		case "phase_sum":
			frontier.Aggregates.Phases.FinalSelect = 0
		case "phase_outcome":
			frontier.Events[1].Phase = "terminal_accept"
			frontier.Aggregates.Phases.FinalSelect = 0
			frontier.Aggregates.Phases.TerminalAccept++
		case "first_reject_missing":
			makeSecondEventReject()
			frontier.FirstRejects = nil
		case "first_reject_not_exact":
			makeSecondEventReject()
			frontier.FirstRejects[0].Detail = "corrupt copy"
		case "first_reject_nonminimal":
			makeSecondEventReject()
			frontier.Events[0].Phase = "boundary_equivalence"
			frontier.Events[0].Outcome = "rejected"
			frontier.Events[0].RejectReason = "status"
			frontier.Aggregates.Phases.TerminalAccept = 0
			frontier.Aggregates.Phases.BoundaryEquivalence = 2
			frontier.Aggregates.Outcomes.AcceptedHead = 0
			frontier.Aggregates.Outcomes.Rejected = 2
			frontier.Aggregates.Rejections.Status = 2
			frontier.Aggregates.AcceptedHeads = 0
		case "first_reject_duplicate_sequence":
			makeSecondEventReject()
			duplicate := frontier.FirstRejects[0]
			duplicate.RejectReason = "score"
			frontier.FirstRejects = append(frontier.FirstRejects, duplicate)
			frontier.Aggregates.Rejections.Score = 1
			frontier.Aggregates.Outcomes.Rejected++
			frontier.Aggregates.Phases.BoundaryEquivalence++
		case "accepted_heads":
			frontier.Aggregates.AcceptedHeads = 0
		case "attempt_not_canonical":
			frontier.Events[0].Attempt = 2
		case "absent_head_data":
			frontier.Events[0].Candidate.State = 1
		case "decision_missing":
			frontier.Events[0].DecisionID = 0
		case "decision_without_heads":
			frontier.Events[0].Target = workCountConvergenceHead{Status: "absent"}
		case "accepted_candidate_present":
			frontier.Events[0].Candidate = workCountConvergenceHead{Status: "live"}
		case "final_candidate_present":
			frontier.Events[1].Candidate = workCountConvergenceHead{Status: "live"}
		case "head_link_without_gss":
			frontier.Events[1].Target.LinkCount = 1
		case "head_hash_without_gss":
			frontier.Events[1].Target.HeadContentHash = 1
		case "head_cost_not_comparable":
			frontier.Events[1].Target.ErrorCost = 1
		case "head_link_cap_not_saturated":
			frontier.Events[1].Target.HasGSS = true
			frontier.Events[1].Target.LinkCountCapped = true
			frontier.Events[1].Target.LinkCount = math.MaxUint16 - 1
		case "reversed_lookahead":
			frontier.Events[0].LookaheadStartByte = 2
			frontier.Events[0].LookaheadEndByte = 1
		case "unset_lookahead_data":
			frontier.Events[0].LookaheadSymbol = 1
		case "scanner_noncomparable_hash":
			frontier.Events[0].ElectionScannerCheckpointHash = 1
		case "scanner_absent_checkpoint_hash":
			frontier.Events[0].ElectionScannerComparable = true
			frontier.Events[0].ElectionScannerCheckpointHash = 1
		case "top_level_capped_or":
			frontier.Events[0].SnapshotCapped = true
		case "top_level_unknown_or":
			frontier.Events[0].SnapshotUnknown = true
		case "packed_sentinel_unproven":
			frontier.Events[1].PathsBefore = 7
			frontier.Aggregates.PathsBeforeTotal = 7
		case "scalar_total":
			frontier.Aggregates.CandidateCountBeforeTotal = 1
		case "packed_roots":
			frontier.Events[1].Phase = "final_expand"
			frontier.Events[1].Outcome = "packed_root_emitted"
			frontier.Events[1].PathsAfter = 2
			frontier.Aggregates.Phases.FinalSelect = 0
			frontier.Aggregates.Phases.FinalExpand = 1
			frontier.Aggregates.Outcomes.Selected = 0
			frontier.Aggregates.Outcomes.PackedRoot = 1
			frontier.Aggregates.PathsAfterTotal = 2
			frontier.Aggregates.PackedRoots = 7
		case "lifecycle_orphan_candidate":
			frontier.Events = frontier.Events[:3]
			frontier.EventsSeen = 3
			frontier.Aggregates.Phases.PostReducePrimary = 1
			frontier.Aggregates.Outcomes.Packed = 0
			frontier.Aggregates.CandidateCountBeforeTotal = 2
			frontier.Aggregates.CandidateCountAfterTotal = 2
		case "lifecycle_duplicate_terminal":
			duplicate := frontier.Events[3]
			duplicate.Sequence = 5
			frontier.Events = append(frontier.Events, duplicate)
			frontier.EventsSeen = 5
			frontier.Aggregates.Phases.PostReducePrimary++
			frontier.Aggregates.Outcomes.Packed++
			frontier.Aggregates.CandidateCountBeforeTotal += 2
			frontier.Aggregates.CandidateCountAfterTotal++
		case "lifecycle_terminal_without_candidate":
			frontier.Events[2].Outcome = "packed"
			frontier.Aggregates.Outcomes.Candidate--
			frontier.Aggregates.Outcomes.Packed++
		case "lifecycle_phase_mismatch":
			frontier.Events[3].Phase = "post_reduce_pending"
			frontier.Aggregates.Phases.PostReducePrimary--
			frontier.Aggregates.Phases.PostReducePending++
		case "unknown_frontier_field", "wrong_snapshot_type", "wrong_head_type", "wrong_aggregate_type":
			workCountValidateGoConvergenceObject(t, workCountCorruptConvergenceJSON(t, frontier, corruption))
			return
		default:
			t.Fatalf("unknown corruption case %q", corruption)
		}
		workCountValidateGoConvergence(t, frontier)
		return
	}

	for _, corruption := range []string{
		"retained_short", "sequence_gap", "phase_sum", "phase_outcome", "first_reject_missing", "first_reject_not_exact", "first_reject_nonminimal", "first_reject_duplicate_sequence", "accepted_heads", "attempt_not_canonical",
		"absent_head_data", "decision_missing", "decision_without_heads", "accepted_candidate_present", "final_candidate_present",
		"head_link_without_gss", "head_hash_without_gss", "head_cost_not_comparable", "head_link_cap_not_saturated",
		"reversed_lookahead", "unset_lookahead_data", "scanner_noncomparable_hash", "scanner_absent_checkpoint_hash",
		"top_level_capped_or", "top_level_unknown_or", "packed_sentinel_unproven", "scalar_total", "packed_roots",
		"lifecycle_orphan_candidate", "lifecycle_duplicate_terminal", "lifecycle_terminal_without_candidate", "lifecycle_phase_mismatch",
		"unknown_frontier_field", "wrong_snapshot_type", "wrong_head_type", "wrong_aggregate_type",
	} {
		t.Run(corruption, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestWorkCountConvergenceAdmissionRejectsCorruption$", "-test.count=1")
			cmd.Env = append(os.Environ(), workCountConvergenceCorruptionEnv+"="+corruption, "GTS_PARITY_ALLOW_HOST=1")
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("corruption %q passed fail-closed admission: %s", corruption, output)
			}
		})
	}
}

func TestWorkCountConvergenceAdmissionAcceptsUnknownPackedPathSentinel(t *testing.T) {
	frontier := workCountValidConvergenceForTest()
	frontier.Events[1].PathsBefore = 7
	frontier.Events[1].SnapshotUnknown = true
	frontier.SnapshotCountUnknown = true
	frontier.Aggregates.PathsBeforeTotal = 7
	workCountValidateGoConvergence(t, frontier)
}

func TestWorkCountConvergenceAdmissionAcceptsOverlappingStatusBitsByPriority(t *testing.T) {
	frontier := workCountValidConvergenceForTest()
	head := &frontier.Events[1].Target
	head.Status, head.Dead, head.Accepted, head.Paused = "dead", true, true, true
	workCountValidateGoConvergence(t, frontier)
	head.Status, head.Dead = "accepted", false
	workCountValidateGoConvergence(t, frontier)
}

func TestWorkCountConvergenceAdmissionAcceptsTruncatedAggregateLowerBounds(t *testing.T) {
	frontier := workCountValidConvergenceForTest()
	template := frontier.Events[1]
	for sequence := uint64(5); sequence <= 256; sequence++ {
		event := template
		event.Sequence = sequence
		event.DecisionID = uint32(sequence)
		frontier.Events = append(frontier.Events, event)
	}
	frontier.EventsSeen = 257
	frontier.EventTruncated = true
	frontier.Aggregates.Phases.FinalSelect += 253
	frontier.Aggregates.Outcomes.Selected += 253
	frontier.Aggregates.CandidateCountBeforeTotal++
	workCountValidateGoConvergence(t, frontier)
}

func TestWorkCountConvergenceManifestMatchesImmutableV2Sections(t *testing.T) {
	repoRoot := workCountRepoRoot(t)
	sharedPath := filepath.Join(repoRoot, "cgo_harness", "work_count", "contract_v2.json")
	convergencePath := filepath.Join(repoRoot, "cgo_harness", "work_count", "contract_v3.json")
	workCountValidateManifest(t, sharedPath)
	workCountValidateConvergenceManifest(t, convergencePath)
	workCountValidateManifestAgreement(t, sharedPath, convergencePath)
	shared, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := os.ReadFile(convergencePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := workCountBytesSHA(shared); got != workCountSharedManifestSHA256 {
		t.Fatalf("immutable v2 manifest sha256=%s want=%s", got, workCountSharedManifestSHA256)
	}
	if got := workCountBytesSHA(convergence); got != workCountConvergenceManifestSHA256 {
		t.Fatalf("v3 manifest sha256=%s want=%s", got, workCountConvergenceManifestSHA256)
	}
	if err := workCountManifestAgreementError(shared, convergence); err != nil {
		t.Fatal(err)
	}

	for _, section := range []string{"direct", "terminal", "proxy", "derived", "attempt_attribution"} {
		var mutated map[string]any
		if err := json.Unmarshal(convergence, &mutated); err != nil {
			t.Fatal(err)
		}
		value, ok := mutated[section].(map[string]any)
		if !ok {
			t.Fatalf("section %s is not an object", section)
		}
		value["__drift_test"] = true
		data, err := json.Marshal(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if err := workCountManifestAgreementError(shared, data); err == nil {
			t.Fatalf("semantic drift in %s was accepted", section)
		}
	}
}

func workCountBytesSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func workCountValidConvergenceForTest() workCountConvergence {
	absent := workCountConvergenceHead{Status: "absent"}
	accepted := workCountConvergenceHead{Status: "accepted", Accepted: true}
	live := workCountConvergenceHead{Status: "live"}
	return workCountConvergence{
		Capability:         "go_only_detailed_convergence",
		MaxEvents:          256,
		MaxPackedPathCount: 7,
		EventsSeen:         4,
		PathWalkWorkBudget: 16 * 1024,
		Events: []workCountConvergenceEvent{
			{Sequence: 1, Attempt: 1, DecisionID: 1, Phase: "terminal_accept", Outcome: "accepted_head", Target: accepted, Candidate: absent},
			{Sequence: 2, Attempt: 1, DecisionID: 2, Phase: "final_select", Outcome: "selected", Target: live, Candidate: absent},
			{Sequence: 3, Attempt: 1, DecisionID: 3, Phase: "post_reduce_primary", Outcome: "candidate", Target: live, Candidate: live, CandidateCountBefore: 2, CandidateCountAfter: 2},
			{Sequence: 4, Attempt: 1, DecisionID: 3, Phase: "post_reduce_primary", Outcome: "packed", Target: live, Candidate: live, CandidateCountBefore: 2, CandidateCountAfter: 1},
		},
		Aggregates: workCountConvergenceTotals{
			Phases:                    workCountConvergencePhaseTotals{TerminalAccept: 1, FinalSelect: 1, PostReducePrimary: 2},
			Outcomes:                  workCountConvergenceOutcomeTotals{AcceptedHead: 1, Selected: 1, Candidate: 1, Packed: 1},
			CandidateCountBeforeTotal: 4,
			CandidateCountAfterTotal:  3,
			AcceptedHeads:             1,
		},
	}
}

func workCountCorruptConvergenceJSON(t *testing.T, frontier workCountConvergence, corruption string) []byte {
	t.Helper()
	data, err := json.Marshal(frontier)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	switch corruption {
	case "unknown_frontier_field":
		raw["unknown"] = true
	case "wrong_snapshot_type":
		raw["snapshot_count_capped"] = "false"
	case "wrong_head_type":
		events := raw["events"].([]any)
		event := events[0].(map[string]any)
		target := event["target"].(map[string]any)
		target["state"] = "0"
	case "wrong_aggregate_type":
		aggregates := raw["aggregates"].(map[string]any)
		aggregates["accepted_heads"] = "1"
	default:
		t.Fatalf("unknown raw corruption case %q", corruption)
	}
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
