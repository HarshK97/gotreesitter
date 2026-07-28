//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

// typstNestedListParitySource comes from typst/list/list-nested in the locked
// tree-sitter-typst corpus.
const typstNestedListParitySource = `- First level.

  - Second level.
    There are multiple paragraphs.

    - Third level.

    Still the same bullet point.

  - Still level 2.

- At the top.

`

func TestTypstNestedListLockedCParity(t *testing.T) {
	runParityCase(
		t,
		parityCase{name: "typst"},
		"locked_nested_list",
		[]byte(typstNestedListParitySource),
	)
}
