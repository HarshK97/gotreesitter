//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestIssue667CEnumListsMatchCReference(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{name: "one", source: "enum E { A };\n"},
		{name: "two", source: "enum E { A, B };\n"},
		{name: "three", source: "enum E { A, B, C };\n"},
		{name: "four", source: "enum E { A, B, C, D };\n"},
		{name: "five", source: "enum E { A, B, C, D, E };\n"},
		{name: "trailing comma", source: "enum E { A, B, C, };\n"},
		{name: "explicit values", source: "enum E { A = 1, B = 2, C = 3 };\n"},
		{name: "typedef", source: "typedef enum { RED, GREEN, BLUE } Colour;\n"},
		{name: "comment before close", source: "enum E { A, B, C /* close */\n};\n"},
		{name: "neighboring declarations", source: "enum First { A, B, C };\nenum Second { D, E, F };\n"},
	}

	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			runParityCase(t, parityCase{name: "c"}, "issue667-"+test.name, []byte(test.source))
		})
	}
}
