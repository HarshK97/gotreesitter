package gotreesitter

import "testing"

// TestReadStringEscapeDecode locks tree-sitter's query-string escape decoding
// (audit finding: "\n" previously compiled to the letter 'n').
func TestReadStringEscapeDecode(t *testing.T) {
	cases := map[string]string{
		`"\n"`:    "\n",
		`"\t\r"`:  "\t\r",
		`"a\\b"`:  "a\\b",
		`"q\"q"`:  "q\"q",
		`"\0"`:    "\x00",
		`"plain"`: "plain",
	}
	for in, want := range cases {
		p := &queryParser{input: in, q: &Query{}}
		got, err := p.readString()
		if err != nil {
			t.Fatalf("readString(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("readString(%q) = %q, want %q", in, got, want)
		}
	}
}
