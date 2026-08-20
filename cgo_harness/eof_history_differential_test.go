//go:build cgo && treesitter_c_parity && gts_derivation_set_census && gts_eof_history_census

package cgoharness

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestEOFAcceptHistoryDifferential records the two compact EOF histories before
// their certified drop. A bounded test-only observer records both locked-C
// roots before selection. The test rejects a grant while the two multisets
// lack a full bijection.
func TestEOFAcceptHistoryDifferential(t *testing.T) {
	if !gotreesitter.EOFAcceptHistoryCensusBuilt() {
		t.Fatal("EOF-history census is absent; build with gts_eof_history_census")
	}

	previousForest := os.Getenv("GOT_GLR_FOREST") != "0"
	gotreesitter.SetGLRForestEnabled(false)
	t.Cleanup(func() { gotreesitter.SetGLRForestEnabled(previousForest) })

	for _, language := range []string{"http", "robot"} {
		language := language
		t.Run(language, func(t *testing.T) {
			source := []byte(grammars.ParseSmokeSample(language))
			entry := grammars.DetectLanguageByName(language)
			if entry == nil || entry.Language == nil {
				t.Fatalf("load %s Go grammar", language)
			}
			goLanguage := entry.Language()
			if goLanguage == nil {
				t.Fatalf("load %s Go grammar", language)
			}
			cLanguage, err := COracleLanguage(language)
			if err != nil {
				t.Fatalf("load %s locked C grammar: %v", language, err)
			}

			gotreesitter.EOFAcceptHistoryCensusReset()
			parser := gotreesitter.NewParser(goLanguage)
			parser.SetAdmissionCandidateRoute(true)
			tree, err := parser.Parse(source)
			if err != nil {
				t.Fatalf("parse compact route: %v", err)
			}
			tree.Release()

			frontiers := gotreesitter.EOFAcceptHistoryCensusSnapshot()
			if len(frontiers) != 1 {
				t.Fatalf("pre-drop EOF frontiers=%d, want 1", len(frontiers))
			}
			frontier := frontiers[0]
			if frontier.Token.Symbol != 0 || frontier.Token.StartByte != frontier.Token.EndByte {
				t.Fatalf("frontier token is not zero-width EOF: %+v", frontier.Token)
			}
			if len(frontier.Heads) != 2 {
				t.Fatalf("pre-drop compact histories=%d, want 2", len(frontier.Heads))
			}

			accepting := 0
			noAction := 0
			var compactShapes []string
			var compactAcceptingShape string
			var compactNoActionShape string
			for _, head := range frontier.Heads {
				if head.Accepting {
					accepting++
				}
				if head.NoAction {
					noAction++
				}
				if head.EnumerationErr != "" || head.EnumerationTruncated {
					t.Fatalf("head %d enumeration: err=%q truncated=%v", head.HeaderIndex, head.EnumerationErr, head.EnumerationTruncated)
				}
				if len(head.Candidates) != 1 {
					t.Fatalf("head %d exact paths=%d, want 1", head.HeaderIndex, len(head.Candidates))
				}
				candidate := head.Candidates[0]
				if candidate.MaterializeErr != "" {
					t.Fatalf("head %d materialization: %s", head.HeaderIndex, candidate.MaterializeErr)
				}
				compactShapes = append(compactShapes, candidate.Shape)
				if head.Accepting {
					compactAcceptingShape = candidate.Shape
				}
				if head.NoAction {
					compactNoActionShape = candidate.Shape
				}
				t.Logf(
					"G2 COMPACT language=%s head=%d accept=%v no-action=%v state=%d byte=%d score=%d branch=%d/%v sha256=%x shape=%s",
					language, head.HeaderIndex, head.Accepting, head.NoAction,
					head.Header.State, head.Header.ByteOffset, candidate.Score,
					candidate.BranchOrder, candidate.HasBranchOrder, candidate.DeepSHA256, candidate.Shape,
				)
			}
			if accepting != 1 || noAction != 1 {
				t.Fatalf("pre-drop roles: accepting=%d no-action=%d, want 1/1", accepting, noAction)
			}

			cReceipt, err := cReconstructVersionSet(cLanguage, source)
			if err != nil {
				t.Fatalf("reconstruct locked-C versions: %v", err)
			}
			if cReceipt.Roots != 2 {
				t.Fatalf("locked-C versions=%d, want 2", cReceipt.Roots)
			}
			if len(cReceipt.Accepts) != 2 {
				t.Fatalf("locked-C accept events=%d, want 2", len(cReceipt.Accepts))
			}
			if cReceipt.Accepts[0].RecoverEOF || len(cReceipt.Accepts[0].Folds) != 0 {
				t.Fatalf("locked-C event 0=%+v, want normal accept with no fold", cReceipt.Accepts[0])
			}
			if !cReceipt.Accepts[1].RecoverEOF || len(cReceipt.Accepts[1].Folds) != 1 {
				t.Fatalf("locked-C event 1=%+v, want recover_eof with one fold", cReceipt.Accepts[1])
			}
			publishedShape := eofHistoryCShape(cReceipt.Published)
			publishedMatches := 0
			for _, shape := range compactShapes {
				if shape == publishedShape {
					publishedMatches++
				}
			}
			if publishedMatches == 0 {
				t.Fatalf("locked-C published root is absent from the compact pre-drop set: %s", publishedShape)
			}
			t.Logf(
				"G2 C language=%s versions=%d max-live=%d winner-known=%v winner=%d published-matches=%d published=%s",
				language, cReceipt.Roots, cReceipt.VersionCountMax,
				cReceipt.WinnerIndexKnown, cReceipt.WinnerIndex,
				publishedMatches, publishedShape,
			)

			cHistory := runEOFAcceptHistoryCOracle(t, language, source)
			t.Logf("G2 C RAW language=%s\n%s", language, strings.TrimSpace(cHistory.Raw))
			if len(cHistory.Versions) != 2 {
				t.Fatalf("instrumented locked-C versions=%d, want 2", len(cHistory.Versions))
			}
			if cHistory.Published != publishedShape {
				t.Fatalf("instrumented C publication differs from the unmodified locked-C publication: instrumented=%s unmodified=%s", cHistory.Published, publishedShape)
			}
			if cHistory.Summary != "GTS_C_EOF_SUMMARY captures=2 failed=0" {
				t.Fatalf("instrumented C summary=%q", cHistory.Summary)
			}

			cShapes := make([]string, len(cHistory.Versions))
			var cPublished *eofHistoryCVersion
			var cLosing *eofHistoryCVersion
			for index, version := range cHistory.Versions {
				if version.AcceptIndex != index {
					t.Fatalf("C capture %d has accept index %d; root/event order drifted", index, version.AcceptIndex)
				}
				cShapes[index] = version.Shape
				if version.Precedence != 0 {
					t.Fatalf("C version %d precedence=%d, want 0", index, version.Precedence)
				}
				if version.Shape == cHistory.Published {
					copy := version
					cPublished = &copy
				} else {
					copy := version
					cLosing = &copy
				}
				t.Logf(
					"G2 C ROOT language=%s capture=%d version=%d precedence=%d error-cost=%d shape=%s",
					language, version.AcceptIndex, version.Version,
					version.Precedence, version.ErrorCost, version.Shape,
				)
			}

			compactMultiset := append([]string(nil), compactShapes...)
			cMultiset := append([]string(nil), cShapes...)
			sort.Strings(compactMultiset)
			sort.Strings(cMultiset)
			if equalStrings(compactMultiset, cMultiset) {
				t.Fatal("pre-drop compact histories unexpectedly reached a full locked-C bijection; adjudicate before any production proposal")
			}
			if compactAcceptingShape != cHistory.Published {
				t.Fatalf("compact accepting history does not equal the C-published version")
			}
			if cPublished == nil || cPublished.AcceptIndex != 0 {
				t.Fatalf("published C root does not map to normal event 0: %+v", cPublished)
			}
			if cReceipt.Accepts[cPublished.AcceptIndex].RecoverEOF {
				t.Fatalf("published C root maps to recover_eof: capture=%+v event=%+v", *cPublished, cReceipt.Accepts[cPublished.AcceptIndex])
			}
			if cLosing == nil {
				t.Fatal("instrumented C receipt has no distinct losing version")
			}
			if cLosing.AcceptIndex != 1 || !cReceipt.Accepts[cLosing.AcceptIndex].RecoverEOF {
				t.Fatalf("losing C root does not map to recover_eof event 1: capture=%+v event=%+v", *cLosing, cReceipt.Accepts[cLosing.AcceptIndex])
			}
			if compactNoActionShape == cLosing.Shape {
				t.Fatal("compact no-action history unexpectedly equals the losing C version; adjudicate the new bijection")
			}
			if !strings.HasPrefix(cLosing.Shape, "(ERROR[") || cLosing.ErrorCost == 0 {
				t.Fatalf("losing C version is not the expected error-cost root: %+v", *cLosing)
			}
			compactPayload, err := eofHistoryRootChildrenShape(compactNoActionShape)
			if err != nil {
				t.Fatalf("decode compact no-action payload: %v", err)
			}
			cPayload, err := eofHistoryRootChildrenShape(cLosing.Shape)
			if err != nil {
				t.Fatalf("decode losing C payload: %v", err)
			}
			if compactPayload != cPayload {
				t.Fatalf("no-action payload differs before the C recovery wrapper: compact=%s C=%s", compactPayload, cPayload)
			}
			t.Logf(
				"G2 BIJECTION language=%s status=REJECT cardinality=%d/%d accepting-match=true no-action-payload-match=true c-topology=normal[0]/recover_eof[1] compact-loser=%s c-loser=%s c-loser-error-cost=%d",
				language, len(compactShapes), len(cShapes), compactNoActionShape, cLosing.Shape, cLosing.ErrorCost,
			)
		})
	}
}

func eofHistoryRootChildrenShape(shape string) (string, error) {
	if len(shape) < 2 || shape[0] != '(' || shape[len(shape)-1] != ')' {
		return "", fmt.Errorf("invalid root shape %q", shape)
	}
	depth := 0
	for index := 0; index < len(shape)-1; index++ {
		switch shape[index] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ':
			if depth == 1 {
				return shape[index+1 : len(shape)-1], nil
			}
		}
	}
	return "", nil
}

func eofHistoryCShape(node *dsTree) string {
	if node == nil {
		return "(nil)"
	}
	var builder strings.Builder
	var write func(*dsTree)
	write = func(current *dsTree) {
		if current == nil {
			builder.WriteString("(nil)")
			return
		}
		builder.WriteByte('(')
		if current.Field != "" {
			builder.WriteString(current.Field)
			builder.WriteByte(':')
		}
		builder.WriteString(current.Type)
		fmt.Fprintf(&builder, "[%d-%d]", current.Start, current.End)
		if !current.Named {
			builder.WriteString("!anon")
		}
		if current.Extra {
			builder.WriteString("!extra")
		}
		if current.Missing {
			builder.WriteString("!missing")
		}
		for _, child := range current.Children {
			builder.WriteByte(' ')
			write(child)
		}
		builder.WriteByte(')')
	}
	write(node)
	return builder.String()
}
