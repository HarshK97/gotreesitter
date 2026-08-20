//go:build cgo && treesitter_c_parity && gts_derivation_set_census && gts_eof_history_census && gts_eof_recovery_shadow

package cgoharness

import (
	"os"
	"sort"
	"strings"
	"testing"
	"unsafe"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

// TestEOFRecoveryShadowDifferential compares one private compact recover_eof
// fold with the ordered locked-C accept set. It changes no serving route.
func TestEOFRecoveryShadowDifferential(t *testing.T) {
	previousForest := os.Getenv("GOT_GLR_FOREST") != "0"
	gotreesitter.SetGLRForestEnabled(false)
	t.Cleanup(func() { gotreesitter.SetGLRForestEnabled(previousForest) })

	for _, test := range []struct {
		language  string
		errorCost uint32
	}{
		{language: "http", errorCost: 640},
		{language: "robot", errorCost: 1027},
	} {
		test := test
		t.Run(test.language, func(t *testing.T) {
			source := []byte(grammars.ParseSmokeSample(test.language))
			entry := grammars.DetectLanguageByName(test.language)
			if entry == nil || entry.Language == nil || entry.Language() == nil {
				t.Fatalf("load %s Go grammar", test.language)
			}
			parser := gotreesitter.NewParser(entry.Language())
			parser.SetAdmissionCandidateRoute(true)
			gotreesitter.EOFAcceptHistoryCensusReset()
			tree, err := parser.Parse(source)
			if err != nil {
				t.Fatalf("parse compact route: %v", err)
			}
			tree.Release()

			frontiers := gotreesitter.EOFAcceptHistoryCensusSnapshot()
			if len(frontiers) != 1 || len(frontiers[0].Heads) != 2 {
				t.Fatalf("compact EOF frontier=%d heads=%d, want 1/2", len(frontiers), eofShadowHeadCount(frontiers))
			}
			var accepting *gotreesitter.EOFAcceptHistoryHead
			var noAction *gotreesitter.EOFAcceptHistoryHead
			for index := range frontiers[0].Heads {
				head := &frontiers[0].Heads[index]
				switch {
				case head.Accepting:
					accepting = head
				case head.NoAction:
					noAction = head
				}
			}
			if accepting == nil || noAction == nil || len(accepting.Candidates) != 1 || len(noAction.Candidates) != 1 {
				t.Fatalf("compact EOF roles or exact derivations are incomplete")
			}
			shadow := noAction.RecoveryShadow
			if shadow == nil || shadow.Error != "" {
				t.Fatalf("private EOF recovery receipt=%+v", shadow)
			}
			if shadow.Kind != "recover_eof" || shadow.AcceptIndex != 1 {
				t.Fatalf("private EOF recovery event=%s[%d], want recover_eof[1]", shadow.Kind, shadow.AcceptIndex)
			}
			if shadow.Steps != 1 || shadow.MaxSteps != 1 || shadow.Payloads == 0 || shadow.Payloads > shadow.MaxPayloads {
				t.Fatalf("private EOF recovery bounds=%+v", *shadow)
			}
			assertEOFShadowMemory(t, shadow)
			if !shadow.MutableStorageDisjoint || !shadow.CopiedArenaPrefixesEqual ||
				!shadow.CopiedHeadersEqual || !shadow.MetadataConstructionUnauthenticated || !shadow.RootChildrenExact ||
				!shadow.SchedulerFrameDetached || !shadow.SourceProvidersDetached {
				t.Fatalf("private EOF recovery clone proof=%+v", *shadow)
			}
			if !shadow.ProviderConstructedFromIsolatedParser || !shadow.ProviderSharesImmutableLanguageTables ||
				!shadow.ProviderReductionPlanOnly || !shadow.IsolatedReductionPlansAttached ||
				!shadow.ProviderDiffersFromLive || !shadow.ProviderTableViewDetached ||
				!shadow.ProviderSelectedStoreDetached {
				t.Fatalf("private EOF recovery provider proof=%+v", *shadow)
			}
			if !shadow.ValidationScratchReserved || !shadow.ValidationScratchNoGrowth {
				t.Fatalf("private EOF recovery validation scratch proof=%+v", *shadow)
			}
			if !shadow.SourceSchedulerActive || !shadow.IsolatedParser || !shadow.IsolatedScratch || !shadow.SharedLanguagePointer ||
				!shadow.LiveHeaderStatsWorkUnchanged {
				t.Fatalf("private EOF recovery live isolation=%+v", *shadow)
			}
			if shadow.SubtreesAfter != shadow.SubtreesBefore+1 ||
				shadow.ChildrenAfter != shadow.ChildrenBefore+shadow.Payloads {
				t.Fatalf("private EOF recovery arena delta=%+v", *shadow)
			}
			assertEOFShadowWork(t, shadow)
			if shadow.StartByte != 0 || shadow.EndByte != uint32(len(source)) {
				t.Fatalf("private EOF recovery span=%d..%d, want 0..%d", shadow.StartByte, shadow.EndByte, len(source))
			}
			if shadow.RootSymbol != 65535 || !shadow.RootNamed || shadow.RootExtra || shadow.RootMissing ||
				!shadow.RootIsError || !shadow.RootHasError || shadow.RootDynamicPrecedence != 0 {
				t.Fatalf("private EOF recovery root metadata=%+v", *shadow)
			}
			if !strings.HasPrefix(shadow.RootShape, "(ERROR[") {
				t.Fatalf("private EOF recovery root=%s", shadow.RootShape)
			}
			if shadow.ErrorCost != test.errorCost {
				t.Fatalf("private EOF recovery error cost=%d, want %d", shadow.ErrorCost, test.errorCost)
			}

			noActionPayload, err := eofHistoryRootChildrenShape(noAction.Candidates[0].Shape)
			if err != nil {
				t.Fatalf("decode compact no-action payload: %v", err)
			}
			shadowPayload, err := eofHistoryRootChildrenShape(shadow.RootShape)
			if err != nil {
				t.Fatalf("decode compact recovery payload: %v", err)
			}
			if shadowPayload != noActionPayload {
				t.Fatalf("private recovery changed the pre-recovery payload: before=%s after=%s", noActionPayload, shadowPayload)
			}

			cLanguage, err := COracleLanguage(test.language)
			if err != nil {
				t.Fatalf("load %s locked C grammar: %v", test.language, err)
			}
			cEvents, err := cReconstructVersionSet(cLanguage, source)
			if err != nil {
				t.Fatalf("reconstruct locked-C versions: %v", err)
			}
			if len(cEvents.Accepts) != 2 || cEvents.Accepts[0].RecoverEOF || len(cEvents.Accepts[0].Folds) != 0 ||
				!cEvents.Accepts[1].RecoverEOF || len(cEvents.Accepts[1].Folds) != 1 {
				t.Fatalf("locked-C ordered accepts=%+v", cEvents.Accepts)
			}
			cHistory := runEOFAcceptHistoryCOracle(t, test.language, source)
			if len(cHistory.Versions) != 2 || cHistory.Versions[0].AcceptIndex != 0 || cHistory.Versions[1].AcceptIndex != 1 {
				t.Fatalf("locked-C ordered roots=%+v", cHistory.Versions)
			}

			compactOrdered := []string{accepting.Candidates[0].Shape, shadow.RootShape}
			cOrdered := []string{cHistory.Versions[0].Shape, cHistory.Versions[1].Shape}
			for index := range compactOrdered {
				if compactOrdered[index] != cOrdered[index] {
					t.Fatalf("ordered accept %d differs: compact=%s C=%s", index, compactOrdered[index], cOrdered[index])
				}
			}
			compactMultiset := append([]string(nil), compactOrdered...)
			cMultiset := append([]string(nil), cOrdered...)
			sort.Strings(compactMultiset)
			sort.Strings(cMultiset)
			if !equalStrings(compactMultiset, cMultiset) {
				t.Fatalf("complete root multiset differs: compact=%v C=%v", compactMultiset, cMultiset)
			}
			if shadow.ErrorCost != cHistory.Versions[1].ErrorCost {
				t.Fatalf("recover_eof error cost=%d, want C %d", shadow.ErrorCost, cHistory.Versions[1].ErrorCost)
			}
			t.Logf(
				"G3R BIJECTION language=%s status=PASS ordered=normal[0]/recover_eof[1] error-cost=%d steps=%d/%d payloads=%d/%d peak=%d/%d shadow-sha256=%x",
				test.language, shadow.ErrorCost, shadow.Steps, shadow.MaxSteps,
				shadow.Payloads, shadow.MaxPayloads, shadow.PeakCloneBytes,
				shadow.MaxCloneBytes, shadow.DeepSHA256,
			)
		})
	}
}

func eofShadowHeadCount(frontiers []gotreesitter.EOFAcceptHistoryFrontier) int {
	if len(frontiers) == 0 {
		return 0
	}
	return len(frontiers[0].Heads)
}

func assertEOFShadowMemory(t *testing.T, shadow *gotreesitter.EOFRecoveryShadowReceipt) {
	t.Helper()
	if shadow.RetainedSelectedPolicy || shadow.CheckpointMapEntries != 0 {
		t.Fatalf("private EOF recovery accepted unsupported retained state=%+v", *shadow)
	}
	if shadow.ProviderWrapperBytes != 8 {
		t.Fatalf("private EOF recovery provider wrapper bytes=%d, want 8", shadow.ProviderWrapperBytes)
	}
	wantTemporary := uint64(shadow.ValidationStructuralPositions)*uint64(unsafe.Sizeof(uint16(0))) +
		uint64(shadow.ValidationRemappedFields)*uint64(unsafe.Sizeof(core.FieldMapEntry{})) +
		uint64(shadow.ValidationRemappedAliases)*uint64(unsafe.Sizeof(core.Symbol(0)))
	if shadow.MapBytes != 0 || shadow.TemporaryBytes != wantTemporary || shadow.PreservationBytes != 0 {
		t.Fatalf("private EOF recovery unexpected auxiliary allocation=%+v", *shadow)
	}
	want := shadow.CoreHeaderBytes + shadow.CopiedArenaBytes + shadow.AppendReserveBytes +
		shadow.MapBytes + shadow.TemporaryBytes + shadow.PreservationBytes + shadow.ProviderWrapperBytes
	if shadow.PeakCloneBytes != want || shadow.PeakCloneBytes == 0 || shadow.PeakCloneBytes > shadow.MaxCloneBytes {
		t.Fatalf("private EOF recovery peak accounting=%+v want=%d", *shadow, want)
	}
}

func assertEOFShadowWork(t *testing.T, shadow *gotreesitter.EOFRecoveryShadowReceipt) {
	t.Helper()
	before := shadow.WorkBefore
	after := shadow.WorkAfter
	if after.ParentConstructionsProxy != before.ParentConstructionsProxy+1 {
		t.Fatalf("private EOF recovery parent work=%d..%d, want +1", before.ParentConstructionsProxy, after.ParentConstructionsProxy)
	}
	after.ParentConstructionsProxy--
	if after != before || shadow.WorkAfter.Overflow {
		t.Fatalf("private EOF recovery changed unexpected work: before=%+v after=%+v", before, shadow.WorkAfter)
	}
}
