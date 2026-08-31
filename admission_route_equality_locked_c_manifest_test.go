//go:build !gts_no_parsercorephase0

package gotreesitter_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
)

const (
	routeEqualityLockedCManifestPath    = "cgo_harness/testdata/compact_route_locked_c_exceptions_v1.json"
	routeEqualityLockedCManifestSchema  = "compact-route-locked-c-exceptions-v1"
	routeEqualityLockedCManifestVersion = 1
	routeEqualityLockedCWitnessCount    = 2
)

var routeEqualityLockedCWitnessIDs = map[string]bool{
	"erlang_issue984_no_newline":       true,
	"erlang_issue984_trailing_newline": true,
}

type routeEqualityLockedCManifest struct {
	Schema        string                        `json:"schema"`
	Version       int                           `json:"version"`
	TrackingIssue string                        `json:"tracking_issue"`
	WitnessCount  int                           `json:"witness_count"`
	Witnesses     []routeEqualityLockedCWitness `json:"witnesses"`
}

type routeEqualityLockedCWitness struct {
	ID           string                       `json:"id"`
	Language     string                       `json:"language"`
	SourceUTF8   string                       `json:"source_utf8"`
	SourceBytes  int                          `json:"source_bytes"`
	SourceSHA256 string                       `json:"source_sha256"`
	COracle      routeEqualityLockedCOracle   `json:"c_oracle"`
	Expected     routeEqualityLockedCExpected `json:"expected"`
}

type routeEqualityLockedCOracle struct {
	Contract              string `json:"contract"`
	BindingModule         string `json:"binding_module"`
	BindingVersion        string `json:"binding_version"`
	BindingCommit         string `json:"binding_commit"`
	RuntimeVersion        string `json:"runtime_version"`
	RuntimeCommit         string `json:"runtime_commit"`
	GrammarRepository     string `json:"grammar_repository"`
	GrammarCommit         string `json:"grammar_commit"`
	CompilerPath          string `json:"compiler_path"`
	CompilerVersion       string `json:"compiler_version"`
	GrammarCompileFlags   string `json:"grammar_compile_flags"`
	GrammarArtifactSHA256 string `json:"grammar_artifact_sha256"`
}

type routeEqualityLockedCExpected struct {
	CHasError              *bool  `json:"c_has_error"`
	CompactHasError        *bool  `json:"compact_has_error"`
	ProductionHasError     *bool  `json:"production_has_error"`
	CompactCDeepSHA256     string `json:"compact_c_deep_sha256"`
	ProductionDeepSHA256   string `json:"production_deep_sha256"`
	CompactRootChildren    int    `json:"compact_root_children"`
	ProductionRootChildren int    `json:"production_root_children"`
}

func loadRouteEqualityLockedCWitnesses(t testing.TB) []routeEqualityLockedCWitness {
	t.Helper()
	raw, err := os.ReadFile(routeEqualityLockedCManifestPath)
	if err != nil {
		t.Fatalf("read locked C exception manifest: %v", err)
	}
	var manifest routeEqualityLockedCManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode locked C exception manifest: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("locked C exception manifest has trailing data: %v", err)
	}
	if manifest.Schema != routeEqualityLockedCManifestSchema || manifest.Version != routeEqualityLockedCManifestVersion {
		t.Fatalf("locked C exception manifest identity=%q/%d, want %q/%d", manifest.Schema, manifest.Version, routeEqualityLockedCManifestSchema, routeEqualityLockedCManifestVersion)
	}
	if manifest.TrackingIssue != "https://github.com/odvcencio/gotreesitter/issues/984" {
		t.Fatalf("locked C exception tracking issue=%q, want issue #984", manifest.TrackingIssue)
	}
	if manifest.WitnessCount != routeEqualityLockedCWitnessCount || len(manifest.Witnesses) != routeEqualityLockedCWitnessCount {
		t.Fatalf("locked C exception witness counts=%d/%d, want %d/%d", manifest.WitnessCount, len(manifest.Witnesses), routeEqualityLockedCWitnessCount, routeEqualityLockedCWitnessCount)
	}

	seenIDs := make(map[string]bool, len(manifest.Witnesses))
	seenInputs := make(map[string]bool, len(manifest.Witnesses))
	for _, witness := range manifest.Witnesses {
		if !routeEqualityLockedCWitnessIDs[witness.ID] {
			t.Fatalf("unexpected locked C exception ID %q", witness.ID)
		}
		if seenIDs[witness.ID] {
			t.Fatalf("duplicate locked C exception ID %q", witness.ID)
		}
		seenIDs[witness.ID] = true
		key := witness.Language + "\x00" + witness.SourceUTF8
		if seenInputs[key] {
			t.Fatalf("duplicate locked C exception input for %q", witness.ID)
		}
		seenInputs[key] = true
		if witness.Language == "" || witness.SourceBytes != len([]byte(witness.SourceUTF8)) {
			t.Fatalf("locked C exception %q language=%q source_bytes=%d actual=%d", witness.ID, witness.Language, witness.SourceBytes, len([]byte(witness.SourceUTF8)))
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(witness.SourceUTF8))); got != witness.SourceSHA256 {
			t.Fatalf("locked C exception %q source SHA-256=%s, want %s", witness.ID, got, witness.SourceSHA256)
		}
		validateRouteEqualityLockedCDigest(t, witness.ID, "compact/C", witness.Expected.CompactCDeepSHA256)
		validateRouteEqualityLockedCDigest(t, witness.ID, "production", witness.Expected.ProductionDeepSHA256)
		validateRouteEqualityLockedCDigest(t, witness.ID, "grammar artifact", witness.COracle.GrammarArtifactSHA256)
		if witness.Expected.CHasError == nil || witness.Expected.CompactHasError == nil || witness.Expected.ProductionHasError == nil {
			t.Fatalf("locked C exception %q omits a HasError expectation", witness.ID)
		}
		oracleFields := []string{
			witness.COracle.Contract,
			witness.COracle.BindingModule,
			witness.COracle.BindingVersion,
			witness.COracle.BindingCommit,
			witness.COracle.RuntimeVersion,
			witness.COracle.RuntimeCommit,
			witness.COracle.GrammarRepository,
			witness.COracle.GrammarCommit,
			witness.COracle.CompilerPath,
			witness.COracle.CompilerVersion,
			witness.COracle.GrammarCompileFlags,
		}
		for fieldIndex, value := range oracleFields {
			if value == "" {
				t.Fatalf("locked C exception %q C-oracle field %d is empty", witness.ID, fieldIndex)
			}
		}
	}
	for id := range routeEqualityLockedCWitnessIDs {
		if !seenIDs[id] {
			t.Fatalf("locked C exception manifest omits required ID %q", id)
		}
	}
	return manifest.Witnesses
}

func validateRouteEqualityLockedCDigest(t testing.TB, id, label, digest string) {
	t.Helper()
	if len(digest) != sha256.Size*2 {
		t.Fatalf("locked C exception %q %s digest has length %d", id, label, len(digest))
	}
	if _, err := hex.DecodeString(digest); err != nil {
		t.Fatalf("locked C exception %q %s digest is not hexadecimal", id, label)
	}
}
