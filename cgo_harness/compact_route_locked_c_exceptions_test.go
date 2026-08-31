//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

const compactRouteLockedCManifestPath = "testdata/compact_route_locked_c_exceptions_v1.json"

type compactRouteLockedCManifest struct {
	Schema        string                       `json:"schema"`
	Version       int                          `json:"version"`
	TrackingIssue string                       `json:"tracking_issue"`
	WitnessCount  int                          `json:"witness_count"`
	Witnesses     []compactRouteLockedCWitness `json:"witnesses"`
}

type compactRouteLockedCWitness struct {
	ID           string                      `json:"id"`
	Language     string                      `json:"language"`
	SourceUTF8   string                      `json:"source_utf8"`
	SourceBytes  int                         `json:"source_bytes"`
	SourceSHA256 string                      `json:"source_sha256"`
	COracle      compactRouteLockedCOracle   `json:"c_oracle"`
	Expected     compactRouteLockedCExpected `json:"expected"`
}

type compactRouteLockedCOracle struct {
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

type compactRouteLockedCExpected struct {
	CHasError              *bool  `json:"c_has_error"`
	CompactHasError        *bool  `json:"compact_has_error"`
	ProductionHasError     *bool  `json:"production_has_error"`
	CompactCDeepSHA256     string `json:"compact_c_deep_sha256"`
	ProductionDeepSHA256   string `json:"production_deep_sha256"`
	CompactRootChildren    int    `json:"compact_root_children"`
	ProductionRootChildren int    `json:"production_root_children"`
}

func TestCompactRouteLockedCExceptions(t *testing.T) {
	manifest := loadCompactRouteLockedCManifest(t)
	for _, witness := range manifest.Witnesses {
		witness := witness
		t.Run(witness.ID, func(t *testing.T) {
			entry, ok := parityEntriesByName[witness.Language]
			if !ok {
				t.Fatalf("language %q is not registered", witness.Language)
			}
			language := entry.Language()
			cLanguage, err := COracleLanguage(witness.Language)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := COracleIdentity(witness.Language)
			if err != nil {
				t.Fatal(err)
			}
			assertCompactRouteLockedCIdentity(t, witness.COracle, identity)

			source := []byte(witness.SourceUTF8)
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLanguage); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C oracle returned a nil tree")
			}
			defer cTree.Close()
			if got, err := COracleDeepDigest(cTree); err != nil {
				t.Fatal(err)
			} else if got != witness.Expected.CompactCDeepSHA256 {
				t.Fatalf("C deep digest=%s, want %s", got, witness.Expected.CompactCDeepSHA256)
			}

			productionParser := gotreesitter.NewParser(language)
			productionParser.SetAdmissionCandidateRoute(false)
			productionTree, err := productionParser.Parse(source)
			if err != nil {
				t.Fatalf("production parse: %v", err)
			}
			defer productionTree.Release()

			routedBefore, fallbackBefore := gotreesitter.AdmissionCandidateCounters()
			compactParser := gotreesitter.NewParser(language)
			compactParser.SetAdmissionCandidateRoute(true)
			compactTree, err := compactParser.Parse(source)
			if err != nil {
				t.Fatalf("compact parse: %v", err)
			}
			defer compactTree.Release()
			routedAfter, fallbackAfter := gotreesitter.AdmissionCandidateCounters()
			if routedAfter-routedBefore != 1 || fallbackAfter-fallbackBefore != 0 {
				t.Fatalf("route counter delta=%d/%d, want 1/0", routedAfter-routedBefore, fallbackAfter-fallbackBefore)
			}

			cRoot := cTree.RootNode()
			compactRoot := compactTree.RootNode()
			productionRoot := productionTree.RootNode()
			if got, want := cRoot.HasError(), *witness.Expected.CHasError; got != want {
				t.Fatalf("C root HasError=%t, want %t", got, want)
			}
			if got, want := compactRoot.HasError(), *witness.Expected.CompactHasError; got != want {
				t.Fatalf("compact root HasError=%t, want %t", got, want)
			}
			if got, want := productionRoot.HasError(), *witness.Expected.ProductionHasError; got != want {
				t.Fatalf("production root HasError=%t, want %t", got, want)
			}
			if diff := FirstDivergenceDumpV1(compactRoot, language, cRoot); diff != nil {
				t.Fatalf("compact tree diverges from locked C: %+v", diff)
			}
			if diff := firstLockedCTreeFlagDivergence(compactRoot, language, cRoot, "/"+compactRoot.Type(language)); diff != nil {
				t.Fatalf("compact flags diverge from locked C: %v", diff)
			}

			compactInspection, err := benchfixtures.InspectGoTree(compactRoot, language)
			if err != nil {
				t.Fatal(err)
			}
			if compactInspection.SHA256 != witness.Expected.CompactCDeepSHA256 {
				t.Fatalf("compact digest=%s, want C digest %s", compactInspection.SHA256, witness.Expected.CompactCDeepSHA256)
			}
			productionInspection, err := benchfixtures.InspectGoTree(productionRoot, language)
			if err != nil {
				t.Fatal(err)
			}
			if productionInspection.SHA256 != witness.Expected.ProductionDeepSHA256 {
				t.Fatalf("production digest=%s, want %s", productionInspection.SHA256, witness.Expected.ProductionDeepSHA256)
			}
			if diff := FirstDivergenceDumpV1(productionRoot, language, cRoot); diff == nil {
				t.Fatal("production now matches locked C; remove the route-equality exception")
			}
			if compactRoot.ChildCount() != witness.Expected.CompactRootChildren || productionRoot.ChildCount() != witness.Expected.ProductionRootChildren {
				t.Fatalf(
					"root child counts compact=%d production=%d, want %d/%d",
					compactRoot.ChildCount(), productionRoot.ChildCount(),
					witness.Expected.CompactRootChildren, witness.Expected.ProductionRootChildren,
				)
			}
		})
	}
}

func loadCompactRouteLockedCManifest(t *testing.T) compactRouteLockedCManifest {
	t.Helper()
	raw, err := os.ReadFile(compactRouteLockedCManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest compactRouteLockedCManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("manifest has trailing data: %v", err)
	}
	if manifest.Schema != "compact-route-locked-c-exceptions-v1" || manifest.Version != 1 {
		t.Fatalf("manifest identity=%q/%d, want compact-route-locked-c-exceptions-v1/1", manifest.Schema, manifest.Version)
	}
	if manifest.TrackingIssue != "https://github.com/odvcencio/gotreesitter/issues/984" {
		t.Fatalf("tracking issue=%q, want issue #984", manifest.TrackingIssue)
	}
	if manifest.WitnessCount != 2 || len(manifest.Witnesses) != 2 {
		t.Fatalf("witness counts=%d/%d, want 2/2", manifest.WitnessCount, len(manifest.Witnesses))
	}
	seen := make(map[string]bool, len(manifest.Witnesses))
	for _, witness := range manifest.Witnesses {
		if witness.ID == "" || seen[witness.ID] {
			t.Fatalf("empty or duplicate witness ID %q", witness.ID)
		}
		seen[witness.ID] = true
		if witness.SourceBytes != len([]byte(witness.SourceUTF8)) {
			t.Fatalf("witness %q source bytes=%d, want %d", witness.ID, witness.SourceBytes, len([]byte(witness.SourceUTF8)))
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(witness.SourceUTF8))); got != witness.SourceSHA256 {
			t.Fatalf("witness %q source SHA-256=%s, want %s", witness.ID, got, witness.SourceSHA256)
		}
		if witness.Expected.CHasError == nil || witness.Expected.CompactHasError == nil || witness.Expected.ProductionHasError == nil {
			t.Fatalf("witness %q omits a HasError expectation", witness.ID)
		}
	}
	return manifest
}

func assertCompactRouteLockedCIdentity(t *testing.T, want compactRouteLockedCOracle, got COracleBuildIdentity) {
	t.Helper()
	checks := []struct {
		name string
		got  string
		want string
	}{
		{name: "contract", got: got.Contract, want: want.Contract},
		{name: "binding module", got: got.BindingModule, want: want.BindingModule},
		{name: "binding version", got: got.BindingVersion, want: want.BindingVersion},
		{name: "binding commit", got: got.BindingCommit, want: want.BindingCommit},
		{name: "runtime version", got: got.RuntimeVersion, want: want.RuntimeVersion},
		{name: "runtime commit", got: got.RuntimeCommit, want: want.RuntimeCommit},
		{name: "grammar repository", got: got.GrammarRepo, want: want.GrammarRepository},
		{name: "grammar commit", got: got.GrammarCommit, want: want.GrammarCommit},
		{name: "compiler path", got: got.CompilerPath, want: want.CompilerPath},
		{name: "compiler version", got: got.CompilerVersion, want: want.CompilerVersion},
		{name: "grammar compile flags", got: got.GrammarCompileFlags, want: want.GrammarCompileFlags},
		{name: "grammar artifact SHA-256", got: got.GrammarArtifactSHA256, want: want.GrammarArtifactSHA256},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("C oracle %s=%q, want %q", check.name, check.got, check.want)
		}
	}
}
