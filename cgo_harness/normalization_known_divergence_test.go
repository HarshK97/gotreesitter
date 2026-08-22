//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

type normalizationKnownDivergence struct {
	Path     string
	Category string
	GoValue  string
	CValue   string
	Reason   string
}

func normalizationAssertKnownDivergence(
	t *testing.T,
	witness string,
	got *DumpV1Divergence,
	want *normalizationKnownDivergence,
) bool {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf(
				"%s has an unexpected locked-C divergence: path=%q category=%q Go=%q C=%q",
				witness,
				got.Path,
				got.Category,
				got.GoValue,
				got.CValue,
			)
		}
		return true
	}
	if got == nil {
		t.Fatalf(
			"known divergence %q now matches locked C; remove the ratchet after route verification",
			witness,
		)
	}
	if got.Path != want.Path ||
		got.Category != want.Category ||
		got.GoValue != want.GoValue ||
		got.CValue != want.CValue {
		t.Fatalf(
			"known divergence changed for %q: got path=%q category=%q Go=%q C=%q; want path=%q category=%q Go=%q C=%q",
			witness,
			got.Path,
			got.Category,
			got.GoValue,
			got.CValue,
			want.Path,
			want.Category,
			want.GoValue,
			want.CValue,
		)
	}
	t.Skipf(
		"known locked-C divergence for %q: %s; path=%s category=%s Go=%s C=%s",
		witness,
		want.Reason,
		want.Path,
		want.Category,
		want.GoValue,
		want.CValue,
	)
	return false
}
