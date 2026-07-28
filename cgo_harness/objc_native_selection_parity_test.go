//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestObjcNativeAlternativeSelectionParity(t *testing.T) {
	sources := []struct {
		name   string
		source string
	}{
		{name: "encode_type", source: `void f(id coder) { [coder encodeValueOfObjCType: @encode(NSUInteger) at: 0]; }
`},
		{name: "function_pointers", source: `void f() {
  NSComparisonResult (*imp)(id, SEL, id);
  NSComparisonResult (*comp)(id, SEL, id) = 0;
}
`},
	}
	for _, source := range sources {
		t.Run(source.name, func(t *testing.T) {
			testCase := parityCase{name: "objc", source: source.source}
			runParityCase(t, testCase, "objc-native-selection", normalizedSource("objc", source.source))
		})
	}
}
