//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const (
	compactT3WitnessManifestPath    = "testdata/compact_t3_oracle_witnesses_v1.json"
	compactT3WitnessManifestSchema  = "compact-t3-c-oracle-witnesses-v1"
	compactT3WitnessManifestVersion = 1
	compactT3WitnessParityScope     = "has_error_only"
	compactT3WitnessTrackingIssue   = "https://github.com/odvcencio/gotreesitter/issues/587"
	compactT3WitnessCount           = 20
)

var compactT3WitnessLanguageCounts = map[string]int{
	"html":       10,
	"javascript": 8,
	"swift":      2,
}

type compactT3WitnessManifest struct {
	Schema         string             `json:"schema"`
	Version        int                `json:"version"`
	ParityScope    string             `json:"parity_scope"`
	TrackingIssue  string             `json:"tracking_issue"`
	WitnessCount   int                `json:"witness_count"`
	LanguageCounts map[string]int     `json:"language_counts"`
	Witnesses      []compactT3Witness `json:"witnesses"`
}

type compactT3Witness struct {
	ID               string                     `json:"id"`
	Language         string                     `json:"language"`
	FailureClass     string                     `json:"failure_class"`
	SourceProvenance compactT3SourceProvenance  `json:"source_provenance"`
	SourceUTF8       string                     `json:"source_utf8"`
	SourceSHA256     string                     `json:"source_sha256"`
	COracle          compactT3COracleProvenance `json:"c_oracle"`
	Expected         compactT3ExpectedOutcome   `json:"expected"`
}

type compactT3SourceProvenance struct {
	Origin string `json:"origin"`
	Record string `json:"record"`
}

type compactT3COracleProvenance struct {
	Contract              string `json:"contract"`
	BindingCommit         string `json:"binding_commit"`
	RuntimeCommit         string `json:"runtime_commit"`
	GrammarRepository     string `json:"grammar_repository"`
	GrammarCommit         string `json:"grammar_commit"`
	CompilerPath          string `json:"compiler_path"`
	CompilerVersion       string `json:"compiler_version"`
	GrammarCompileFlags   string `json:"grammar_compile_flags"`
	GrammarArtifactSHA256 string `json:"grammar_artifact_sha256"`
}

type compactT3ExpectedOutcome struct {
	CHasError          *bool `json:"c_has_error"`
	ProductionHasError *bool `json:"production_has_error"`
	CompactHasError    *bool `json:"compact_has_error"`
}

// TestCompactT3OracleAdjudication verifies each committed false-clean witness.
// It compares only HasError. It does not assert recovery-tree structure.
func TestCompactT3OracleAdjudication(t *testing.T) {
	manifest := loadCompactT3WitnessManifest(t)
	for _, language := range compactT3WitnessLanguages(manifest) {
		language := language
		t.Run(language, func(t *testing.T) {
			cLanguage, err := ParityCLanguage(language)
			if err != nil {
				t.Fatalf("load C oracle: %v", err)
			}
			identity, err := COracleIdentity(language)
			if err != nil {
				t.Fatalf("read C oracle identity: %v", err)
			}
			goLanguage, ok := compactT3GoLanguage(language)
			if !ok {
				t.Fatalf("manifest uses unsupported language %q", language)
			}

			for _, witness := range compactT3WitnessesForLanguage(manifest, language) {
				witness := witness
				t.Run(witness.FailureClass+"/"+witness.ID, func(t *testing.T) {
					assertCompactT3COracleProvenance(t, witness, identity)
					source := []byte(witness.SourceUTF8)
					cHasError := compactT3CHasError(t, cLanguage, source)
					productionHasError := compactT3GoHasError(t, goLanguage, source, false)
					compactHasError := compactT3GoHasError(t, goLanguage, source, true)

					assertCompactT3Outcome(t, witness, "C oracle", witness.Expected.CHasError, cHasError)
					assertCompactT3Outcome(t, witness, "production", witness.Expected.ProductionHasError, productionHasError)
					assertCompactT3Outcome(t, witness, "compact", witness.Expected.CompactHasError, compactHasError)
				})
			}
		})
	}
}

func loadCompactT3WitnessManifest(t *testing.T) compactT3WitnessManifest {
	t.Helper()
	raw, err := os.ReadFile(compactT3WitnessManifestPath)
	if err != nil {
		t.Fatalf("read witness manifest: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest compactT3WitnessManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode witness manifest: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("decode trailing witness manifest content: %v", err)
	}
	if err := validateCompactT3WitnessManifest(manifest); err != nil {
		t.Fatalf("validate witness manifest: %v", err)
	}
	return manifest
}

func validateCompactT3WitnessManifest(manifest compactT3WitnessManifest) error {
	if manifest.Schema != compactT3WitnessManifestSchema {
		return fmt.Errorf("schema=%q, want %q", manifest.Schema, compactT3WitnessManifestSchema)
	}
	if manifest.Version != compactT3WitnessManifestVersion {
		return fmt.Errorf("version=%d, want %d", manifest.Version, compactT3WitnessManifestVersion)
	}
	if manifest.ParityScope != compactT3WitnessParityScope {
		return fmt.Errorf("parity_scope=%q, want %q", manifest.ParityScope, compactT3WitnessParityScope)
	}
	if manifest.TrackingIssue != compactT3WitnessTrackingIssue {
		return fmt.Errorf("tracking_issue=%q, want %q", manifest.TrackingIssue, compactT3WitnessTrackingIssue)
	}
	if manifest.WitnessCount != compactT3WitnessCount {
		return fmt.Errorf("witness_count=%d, want %d", manifest.WitnessCount, compactT3WitnessCount)
	}
	if len(manifest.Witnesses) != compactT3WitnessCount {
		return fmt.Errorf("decoded witness count=%d, want %d", len(manifest.Witnesses), compactT3WitnessCount)
	}
	if err := validateCompactT3LanguageCounts(manifest.LanguageCounts); err != nil {
		return err
	}

	seenIDs := make(map[string]struct{}, len(manifest.Witnesses))
	actualLanguageCounts := make(map[string]int, len(compactT3WitnessLanguageCounts))
	for _, witness := range manifest.Witnesses {
		if strings.TrimSpace(witness.ID) == "" {
			return errorsForCompactT3Witness(witness, "id is empty")
		}
		if _, exists := seenIDs[witness.ID]; exists {
			return errorsForCompactT3Witness(witness, "id is duplicated")
		}
		seenIDs[witness.ID] = struct{}{}
		if _, ok := compactT3WitnessLanguageCounts[witness.Language]; !ok {
			return errorsForCompactT3Witness(witness, "language is not in the exact witness set")
		}
		actualLanguageCounts[witness.Language]++
		if strings.TrimSpace(witness.FailureClass) == "" {
			return errorsForCompactT3Witness(witness, "failure_class is empty")
		}
		if strings.TrimSpace(witness.SourceProvenance.Origin) == "" || strings.TrimSpace(witness.SourceProvenance.Record) == "" {
			return errorsForCompactT3Witness(witness, "source_provenance is incomplete")
		}
		if witness.SourceUTF8 == "" || !utf8.ValidString(witness.SourceUTF8) {
			return errorsForCompactT3Witness(witness, "source_utf8 is empty or invalid")
		}
		if err := validateCompactT3SourceDigest(witness); err != nil {
			return err
		}
		if err := validateCompactT3ExpectedOutcome(witness); err != nil {
			return err
		}
		if err := validateCompactT3COracleProvenance(witness.COracle); err != nil {
			return errorsForCompactT3Witness(witness, err.Error())
		}
	}
	if err := validateCompactT3LanguageCounts(actualLanguageCounts); err != nil {
		return fmt.Errorf("actual %w", err)
	}
	return nil
}

func validateCompactT3LanguageCounts(counts map[string]int) error {
	if len(counts) != len(compactT3WitnessLanguageCounts) {
		return fmt.Errorf("language count entries=%d, want %d", len(counts), len(compactT3WitnessLanguageCounts))
	}
	for language, want := range compactT3WitnessLanguageCounts {
		if got := counts[language]; got != want {
			return fmt.Errorf("language=%q count=%d, want %d", language, got, want)
		}
	}
	return nil
}

func validateCompactT3SourceDigest(witness compactT3Witness) error {
	if len(witness.SourceSHA256) != sha256.Size*2 {
		return errorsForCompactT3Witness(witness, "source_sha256 has an invalid length")
	}
	if _, err := hex.DecodeString(witness.SourceSHA256); err != nil {
		return errorsForCompactT3Witness(witness, "source_sha256 is not hexadecimal")
	}
	sum := sha256.Sum256([]byte(witness.SourceUTF8))
	if got := hex.EncodeToString(sum[:]); got != witness.SourceSHA256 {
		return errorsForCompactT3Witness(witness, fmt.Sprintf("source_sha256=%s, want %s", witness.SourceSHA256, got))
	}
	return nil
}

func validateCompactT3ExpectedOutcome(witness compactT3Witness) error {
	if witness.Expected.CHasError == nil {
		return errorsForCompactT3Witness(witness, "expected.c_has_error is absent")
	}
	if witness.Expected.ProductionHasError == nil {
		return errorsForCompactT3Witness(witness, "expected.production_has_error is absent")
	}
	if witness.Expected.CompactHasError == nil {
		return errorsForCompactT3Witness(witness, "expected.compact_has_error is absent")
	}
	return nil
}

func validateCompactT3COracleProvenance(provenance compactT3COracleProvenance) error {
	fields := []struct {
		name  string
		value string
	}{
		{"contract", provenance.Contract},
		{"binding_commit", provenance.BindingCommit},
		{"runtime_commit", provenance.RuntimeCommit},
		{"grammar_repository", provenance.GrammarRepository},
		{"grammar_commit", provenance.GrammarCommit},
		{"compiler_path", provenance.CompilerPath},
		{"compiler_version", provenance.CompilerVersion},
		{"grammar_compile_flags", provenance.GrammarCompileFlags},
		{"grammar_artifact_sha256", provenance.GrammarArtifactSHA256},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("c_oracle.%s is empty", field.name)
		}
	}
	if len(provenance.GrammarArtifactSHA256) != sha256.Size*2 {
		return fmt.Errorf("c_oracle.grammar_artifact_sha256 has an invalid length")
	}
	if _, err := hex.DecodeString(provenance.GrammarArtifactSHA256); err != nil {
		return fmt.Errorf("c_oracle.grammar_artifact_sha256 is not hexadecimal")
	}
	return nil
}

func errorsForCompactT3Witness(witness compactT3Witness, message string) error {
	return fmt.Errorf("witness %q (%s): %s", witness.ID, witness.Language, message)
}

func compactT3WitnessLanguages(manifest compactT3WitnessManifest) []string {
	languages := make([]string, 0, len(manifest.LanguageCounts))
	for language := range manifest.LanguageCounts {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	return languages
}

func compactT3WitnessesForLanguage(manifest compactT3WitnessManifest, language string) []compactT3Witness {
	var witnesses []compactT3Witness
	for _, witness := range manifest.Witnesses {
		if witness.Language == language {
			witnesses = append(witnesses, witness)
		}
	}
	return witnesses
}

func compactT3GoLanguage(name string) (*gotreesitter.Language, bool) {
	switch name {
	case "html":
		return grammars.HtmlLanguage(), true
	case "javascript":
		return grammars.JavascriptLanguage(), true
	case "swift":
		return grammars.SwiftLanguage(), true
	default:
		return nil, false
	}
}

func assertCompactT3COracleProvenance(t *testing.T, witness compactT3Witness, actual COracleBuildIdentity) {
	t.Helper()
	want := witness.COracle
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"contract", actual.Contract, want.Contract},
		{"binding_commit", actual.BindingCommit, want.BindingCommit},
		{"runtime_commit", actual.RuntimeCommit, want.RuntimeCommit},
		{"grammar_repository", actual.GrammarRepo, want.GrammarRepository},
		{"grammar_commit", actual.GrammarCommit, want.GrammarCommit},
		{"compiler_path", actual.CompilerPath, want.CompilerPath},
		{"compiler_version", actual.CompilerVersion, want.CompilerVersion},
		{"grammar_compile_flags", actual.GrammarCompileFlags, want.GrammarCompileFlags},
		{"grammar_artifact_sha256", actual.GrammarArtifactSHA256, want.GrammarArtifactSHA256},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("witness %q: C oracle %s=%q, want %q", witness.ID, check.name, check.got, check.want)
		}
	}
}

func compactT3CHasError(t *testing.T, language *sitter.Language, source []byte) bool {
	t.Helper()
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		t.Fatalf("set C oracle language: %v", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil || tree.RootNode() == nil {
		t.Fatal("C oracle parse returned no root")
	}
	defer tree.Close()
	return tree.RootNode().HasError()
}

func compactT3GoHasError(t *testing.T, language *gotreesitter.Language, source []byte, compact bool) bool {
	t.Helper()
	parser := gotreesitter.NewParser(language)
	parser.SetAdmissionCandidateRoute(compact)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse compact=%t: %v", compact, err)
	}
	defer tree.Release()
	return tree.RootNode().HasError()
}

func assertCompactT3Outcome(t *testing.T, witness compactT3Witness, route string, want *bool, got bool) {
	t.Helper()
	if want == nil {
		t.Fatalf("witness %q: %s expectation is absent", witness.ID, route)
	}
	if got != *want {
		t.Fatalf("witness %q: %s HasError=%t, want %t", witness.ID, route, got, *want)
	}
}
