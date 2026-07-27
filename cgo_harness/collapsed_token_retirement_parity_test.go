//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestCollapsedTokenRetirementCOracleParity(t *testing.T) {
	tests := []struct {
		name     string
		language string
		source   string
	}{
		{
			name:     "hcl_wrappers",
			language: "hcl",
			source:   "a {\n b = true\n c = [false]\n d = {}\n}\n",
		},
		{
			name:     "cpon_null",
			language: "cpon",
			source:   "[null,true,false]\n",
		},
		{
			name:     "c_sharp_wrappers",
			language: "c_sharp",
			source:   "public class C { public bool F = false; void M() { var x = global::System.String.Empty; } }\n",
		},
		{
			name:     "powershell_assignment_operators",
			language: "powershell",
			source:   "$a = 1\n$a += 2\n",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			runParityCase(
				t,
				parityCase{name: test.language, source: test.source},
				"collapsed-token-retirement",
				[]byte(test.source),
			)
		})
	}
}
