package gotreesitter_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// makeKDLRecoveryGarbageSource synthesizes a KDL document that reliably
// drives the C-recovery cost-competition machinery: a valid, node-per-line
// KDL prefix truncated 70% of the way through, followed by a long run of
// unterminated-string / mismatched-brace garbage that the parser can never
// resynchronize against. The whole garbage tail is absorbed into one
// continuously growing ERROR region, exactly the shape that makes warm CPU
// profiles of error-bearing parses on fleet-tail languages (kdl, uxntal)
// dominated by cRecoverStrategy1Election / cNodeErrorCost / cStackPrefixAgg /
// cHandleError / cCondenseAndResume (parser_recover_c.go).
func makeKDLRecoveryGarbageSource(nodeCount, garbageRepeats int) []byte {
	var body strings.Builder
	for i := 0; i < nodeCount; i++ {
		fmt.Fprintf(&body, "node%d \"arg%d\" key=%d {\n", i, i, i)
		fmt.Fprintf(&body, "  child%d \"x\"\n", i)
		body.WriteString("}\n")
	}
	valid := body.String()
	cut := int(float64(len(valid)) * 0.7)

	var out strings.Builder
	out.Grow(cut + garbageRepeats*32)
	out.WriteString(valid[:cut])
	for i := 0; i < garbageRepeats; i++ {
		out.WriteString(" }} \"unterminated garbage ][ ")
		fmt.Fprintf(&out, "%d==%d<<>>", i, i*7)
	}
	return []byte(out.String())
}

// BenchmarkKDLRecoveryGarbageSuffix exercises the C-recovery cost-competition
// machinery end to end on a representative fleet-tail-class error-bearing
// parse (see makeKDLRecoveryGarbageSource). This is the recovery-heavy
// counterpart to the clean-parse canonical trio in BENCH.md: those
// benchmarks never enter cHandleError, so they cannot see regressions or
// wins in the recovery cost-competition path (cRecoverStrategy1Election,
// cNodeErrorCost, cStackPrefixAgg, cHandleError, cCondenseAndResume in
// parser_recover_c.go / glr.go).
func BenchmarkKDLRecoveryGarbageSuffix(b *testing.B) {
	lang := grammars.KdlLanguage()
	parser := gotreesitter.NewParser(lang)
	src := makeKDLRecoveryGarbageSource(120, 300)

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tree, err := parser.Parse(src)
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
		root := tree.RootNode()
		if root == nil {
			b.Fatalf("recovery garbage-suffix parse returned nil root")
		}
		if !root.HasError() {
			b.Fatalf("recovery garbage-suffix workload parsed without error: benchmark no longer exercises recovery")
		}
		if got, want := root.EndByte(), uint32(len(src)); got != want {
			b.Fatalf("recovery garbage-suffix parse truncated: root.EndByte=%d want=%d", got, want)
		}
		tree.Release()
	}
}
