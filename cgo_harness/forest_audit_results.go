package cgoharness

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ForestAuditOutcome struct {
	Present        bool   `json:"present"`
	Accepted       bool   `json:"accepted"`
	FullSpan       bool   `json:"full_span"`
	StopReason     string `json:"stop_reason"`
	SourceLen      uint32 `json:"source_len"`
	ExpectedEOF    uint32 `json:"expected_eof_byte"`
	RootEndByte    uint32 `json:"root_end_byte"`
	RootHasError   bool   `json:"root_has_error"`
	Truncated      bool   `json:"truncated"`
	StoppedEarly   bool   `json:"stopped_early"`
	LastTokenEOF   bool   `json:"last_token_was_eof"`
	ForestFastPath bool   `json:"forest_fast_path"`
}

type ForestAuditFileResult struct {
	Path        string             `json:"path"`
	Bytes       int64              `json:"bytes"`
	SHA256      string             `json:"sha256"`
	Forest      ForestAuditOutcome `json:"forest"`
	Peer        ForestAuditOutcome `json:"peer"`
	Disposition string             `json:"disposition"`
	Diff        string             `json:"diff,omitempty"`
	Decline     string             `json:"decline,omitempty"`
}

type ForestAuditResult struct {
	Schema               string                  `json:"schema"`
	Mode                 string                  `json:"mode"`
	GotreesitterRevision string                  `json:"gotreesitter_revision"`
	CorpusManifestSHA256 string                  `json:"corpus_manifest_sha256"`
	CorpusLockSHA256     string                  `json:"corpus_lock_sha256"`
	Language             string                  `json:"language"`
	Status               string                  `json:"status"`
	FilesTotal           int                     `json:"files_total"`
	FilesAccepted        int                     `json:"files_accepted"`
	FilesDeclined        int                     `json:"files_declined"`
	FilesDiverged        int                     `json:"files_diverged"`
	ForestNanos          int64                   `json:"forest_nanos"`
	PeerNanos            int64                   `json:"peer_nanos"`
	Files                []ForestAuditFileResult `json:"files"`
}

type ForestAuditLanguageReport struct {
	Language   string             `json:"language"`
	Status     string             `json:"status"`
	Production *ForestAuditResult `json:"production,omitempty"`
	COracle    *ForestAuditResult `json:"c_oracle,omitempty"`
}

type ForestAuditReport struct {
	Schema               string                      `json:"schema"`
	GotreesitterRevision string                      `json:"gotreesitter_revision"`
	CorpusManifestSHA256 string                      `json:"corpus_manifest_sha256"`
	CorpusLockSHA256     string                      `json:"corpus_lock_sha256"`
	Status               string                      `json:"status"`
	LanguagesExpected    int                         `json:"languages_expected"`
	LanguagesComplete    int                         `json:"languages_complete"`
	Languages            []ForestAuditLanguageReport `json:"languages"`
}

func NewForestAuditResult(mode, revision, manifestPath, lockSHA, language string) (ForestAuditResult, error) {
	digest, err := forestFileSHA256(manifestPath)
	if err != nil {
		return ForestAuditResult{}, fmt.Errorf("hash forest manifest: %w", err)
	}
	return ForestAuditResult{
		Schema:               ForestAuditResultSchema,
		Mode:                 mode,
		GotreesitterRevision: revision,
		CorpusManifestSHA256: digest,
		CorpusLockSHA256:     lockSHA,
		Language:             language,
		Status:               "pass",
	}, nil
}

func WriteForestAuditResult(path string, result ForestAuditResult) error {
	if err := validateForestAuditResult(result); err != nil {
		return err
	}
	return writeForestAuditJSON(path, result)
}

func ReduceForestAuditResults(manifestPath, resultsRoot string) (ForestAuditReport, error) {
	reduction, err := newForestAuditReduction(manifestPath)
	if err != nil {
		return ForestAuditReport{}, err
	}
	if err := reduction.loadResults(resultsRoot); err != nil {
		return ForestAuditReport{}, err
	}
	return reduction.report(), nil
}

type forestAuditReduction struct {
	manifestDigest string
	manifest       ForestCorpusManifest
	languages      []string
	manifestFiles  map[string]map[string]ForestCorpusManifestFile
	results        map[string]map[string]*ForestAuditResult
}

func newForestAuditReduction(manifestPath string) (*forestAuditReduction, error) {
	manifest, err := decodeForestCorpusManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	digest, err := forestFileSHA256(manifestPath)
	if err != nil {
		return nil, err
	}
	reduction := &forestAuditReduction{
		manifestDigest: digest,
		manifest:       manifest,
		manifestFiles:  make(map[string]map[string]ForestCorpusManifestFile),
		results:        make(map[string]map[string]*ForestAuditResult),
	}
	for _, file := range manifest.Files {
		if reduction.manifestFiles[file.Language] == nil {
			reduction.languages = append(reduction.languages, file.Language)
			reduction.manifestFiles[file.Language] = make(map[string]ForestCorpusManifestFile)
		}
		reduction.manifestFiles[file.Language][file.Path] = file
	}
	sort.Strings(reduction.languages)
	return reduction, nil
}

func (reduction *forestAuditReduction) loadResults(resultsRoot string) error {
	return filepath.WalkDir(resultsRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		isReport, err := forestAuditFileIsReport(filePath)
		if err != nil {
			return err
		}
		if isReport {
			return nil
		}
		return reduction.admitResult(filePath)
	})
}

func forestAuditFileIsReport(filePath string) (bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return false, nil
	}
	return header.Schema == ForestAuditReportSchema, nil
}

func (reduction *forestAuditReduction) admitResult(filePath string) error {
	var result ForestAuditResult
	if err := readForestAuditJSON(filePath, &result); err != nil {
		return fmt.Errorf("read result %s: %w", filePath, err)
	}
	if err := validateForestAuditResult(result); err != nil {
		return fmt.Errorf("validate result %s: %w", filePath, err)
	}
	if err := reduction.validateResultIdentity(result); err != nil {
		return fmt.Errorf("result %s: %w", filePath, err)
	}
	if reduction.results[result.Language] == nil {
		reduction.results[result.Language] = make(map[string]*ForestAuditResult)
	}
	if reduction.results[result.Language][result.Mode] != nil {
		return fmt.Errorf("duplicate %s result for %s", result.Mode, result.Language)
	}
	copy := result
	reduction.results[result.Language][result.Mode] = &copy
	return nil
}

func (reduction *forestAuditReduction) validateResultIdentity(result ForestAuditResult) error {
	if result.GotreesitterRevision != reduction.manifest.GotreesitterRevision ||
		result.CorpusManifestSHA256 != reduction.manifestDigest ||
		result.CorpusLockSHA256 != reduction.manifest.CorpusLock.SHA256 {
		return fmt.Errorf("identity does not match manifest")
	}
	expected, ok := reduction.manifestFiles[result.Language]
	if !ok {
		return fmt.Errorf("language %q absent from manifest", result.Language)
	}
	return validateForestAuditResultManifestFiles(result, expected)
}

func (reduction *forestAuditReduction) report() ForestAuditReport {
	report := ForestAuditReport{
		Schema:               ForestAuditReportSchema,
		GotreesitterRevision: reduction.manifest.GotreesitterRevision,
		CorpusManifestSHA256: reduction.manifestDigest,
		CorpusLockSHA256:     reduction.manifest.CorpusLock.SHA256,
		Status:               "pass",
		LanguagesExpected:    len(reduction.languages),
	}
	for _, language := range reduction.languages {
		row := reduction.languageReport(language)
		if row.Status != "incomplete" {
			report.LanguagesComplete++
		}
		report.Status = mergeForestAuditReportStatus(report.Status, row.Status)
		report.Languages = append(report.Languages, row)
	}
	return report
}

func (reduction *forestAuditReduction) languageReport(language string) ForestAuditLanguageReport {
	row := ForestAuditLanguageReport{Language: language, Status: "incomplete"}
	row.Production = reduction.results[language]["production"]
	row.COracle = reduction.results[language]["c_oracle"]
	if row.Production == nil || row.COracle == nil {
		return row
	}
	row.Status = "pass"
	if row.Production.Status != "pass" || row.COracle.Status != "pass" {
		row.Status = "fail"
	}
	return row
}

func mergeForestAuditReportStatus(current, next string) string {
	if current == "fail" || next == "fail" {
		return "fail"
	}
	if current == "incomplete" || next == "incomplete" {
		return "incomplete"
	}
	return "pass"
}

func WriteForestAuditReport(path string, report ForestAuditReport) error {
	return writeForestAuditJSON(path, report)
}

func validateForestAuditResult(result ForestAuditResult) error {
	if err := validateForestAuditResultMetadata(result); err != nil {
		return err
	}
	counts, err := validateForestAuditResultFiles(result.Files)
	if err != nil {
		return err
	}
	if result.FilesTotal != len(result.Files) || result.FilesAccepted != counts.accepted ||
		result.FilesDeclined != counts.declined || result.FilesDiverged != counts.diverged ||
		counts.accepted+counts.declined != result.FilesTotal {
		return fmt.Errorf("incoherent forest audit file counts")
	}
	if (result.FilesDiverged > 0 || result.FilesAccepted == 0) && result.Status != "fail" {
		return fmt.Errorf("forest audit result status must fail with divergences or zero accepted files")
	}
	return nil
}

func validateForestAuditResultMetadata(result ForestAuditResult) error {
	if result.Schema != ForestAuditResultSchema {
		return fmt.Errorf("forest audit result schema %q", result.Schema)
	}
	if result.Mode != "production" && result.Mode != "c_oracle" {
		return fmt.Errorf("invalid forest audit mode %q", result.Mode)
	}
	if err := forestManifestRevision(result.GotreesitterRevision); err != nil {
		return err
	}
	if _, err := forestManifestDigest("manifest", result.CorpusManifestSHA256); err != nil {
		return err
	}
	if _, err := forestManifestDigest("corpus lock", result.CorpusLockSHA256); err != nil {
		return err
	}
	if err := forestManifestLanguage(result.Language); err != nil {
		return err
	}
	if result.Status != "pass" && result.Status != "fail" {
		return fmt.Errorf("invalid forest audit status %q", result.Status)
	}
	return nil
}

type forestAuditResultCounts struct {
	accepted int
	declined int
	diverged int
}

func validateForestAuditResultFiles(files []ForestAuditFileResult) (forestAuditResultCounts, error) {
	var counts forestAuditResultCounts
	seen := make(map[string]bool, len(files))
	previous := ""
	for i, file := range files {
		if err := validateForestAuditResultFile(file, i, previous, seen); err != nil {
			return counts, err
		}
		previous = file.Path
		switch file.Disposition {
		case "accepted":
			counts.accepted++
		case "declined":
			counts.declined++
		case "diverged":
			counts.accepted++
			counts.diverged++
		}
	}
	return counts, nil
}

func validateForestAuditResultFile(file ForestAuditFileResult, index int, previous string, seen map[string]bool) error {
	if _, err := forestManifestPath("/", "result file", file.Path); err != nil {
		return fmt.Errorf("forest audit result file %d: %w", index, err)
	}
	if seen[file.Path] {
		return fmt.Errorf("forest audit result file %d duplicates %q", index, file.Path)
	}
	if previous != "" && file.Path < previous {
		return fmt.Errorf("forest audit result files are not sorted")
	}
	seen[file.Path] = true
	if file.Bytes < 0 {
		return fmt.Errorf("forest audit result file %d has negative byte count", index)
	}
	if _, err := forestManifestDigest("result file", file.SHA256); err != nil {
		return fmt.Errorf("forest audit result file %d: %w", index, err)
	}
	if err := validateForestAuditDisposition(file, index); err != nil {
		return err
	}
	return validateForestAuditOutcomes(file, index)
}

func validateForestAuditDisposition(file ForestAuditFileResult, index int) error {
	switch file.Disposition {
	case "accepted":
		return nil
	case "declined":
		if strings.TrimSpace(file.Decline) == "" {
			return fmt.Errorf("forest audit result file %d declined without reason", index)
		}
		return nil
	case "diverged":
		if strings.TrimSpace(file.Diff) == "" {
			return fmt.Errorf("forest audit result file %d diverged without diff", index)
		}
		return nil
	default:
		return fmt.Errorf("forest audit result file %d has invalid disposition %q", index, file.Disposition)
	}
}

func validateForestAuditOutcomes(file ForestAuditFileResult, index int) error {
	if file.Bytes <= math.MaxUint32 {
		want := uint32(file.Bytes)
		if file.Forest.SourceLen != want || file.Forest.ExpectedEOF != want {
			return fmt.Errorf("forest audit result file %d forest source length does not match authenticated bytes", index)
		}
		if file.Peer.SourceLen != want || file.Peer.ExpectedEOF != want {
			return fmt.Errorf("forest audit result file %d peer source length does not match authenticated bytes", index)
		}
	}
	if err := validateForestAuditOutcomeCoherence(file.Forest, "forest", index); err != nil {
		return err
	}
	if err := validateForestAuditOutcomeCoherence(file.Peer, "peer", index); err != nil {
		return err
	}
	switch file.Disposition {
	case "accepted":
		if err := validateForestAcceptedOutcome(file.Forest, index); err != nil {
			return err
		}
		if !file.Peer.Accepted || !file.Peer.FullSpan {
			return fmt.Errorf("forest audit result file %d accepted without accepted full-span peer", index)
		}
	case "declined":
		if file.Forest.Accepted {
			return fmt.Errorf("forest audit result file %d declined despite accepted forest outcome", index)
		}
	case "diverged":
		return validateForestAcceptedOutcome(file.Forest, index)
	}
	return nil
}

func validateForestAuditOutcomeCoherence(outcome ForestAuditOutcome, role string, index int) error {
	if outcome.Accepted && (!outcome.Present || !outcome.FullSpan || outcome.Truncated || outcome.StoppedEarly || outcome.RootHasError) {
		return fmt.Errorf("forest audit result file %d %s accepted outcome is incoherent", index, role)
	}
	if outcome.FullSpan && (!outcome.Present || outcome.Truncated) {
		return fmt.Errorf("forest audit result file %d %s full-span outcome is incoherent", index, role)
	}
	if outcome.Truncated && (!outcome.Present || outcome.FullSpan) {
		return fmt.Errorf("forest audit result file %d %s truncated outcome is incoherent", index, role)
	}
	if !outcome.Present && (outcome.RootHasError || outcome.StoppedEarly || outcome.LastTokenEOF || outcome.ForestFastPath) {
		return fmt.Errorf("forest audit result file %d %s absent outcome carries parse state", index, role)
	}
	return nil
}

func validateForestAcceptedOutcome(outcome ForestAuditOutcome, index int) error {
	if !outcome.Present || !outcome.Accepted || !outcome.FullSpan || !outcome.LastTokenEOF ||
		outcome.RootHasError || outcome.Truncated || outcome.StoppedEarly || !outcome.ForestFastPath {
		return fmt.Errorf("forest audit result file %d accepted without complete forest-fast-path outcome", index)
	}
	return nil
}

func validateForestAuditResultManifestFiles(result ForestAuditResult, expected map[string]ForestCorpusManifestFile) error {
	if len(result.Files) != len(expected) {
		return fmt.Errorf("result has %d files, manifest requires %d", len(result.Files), len(expected))
	}
	for _, file := range result.Files {
		want, ok := expected[file.Path]
		if !ok {
			return fmt.Errorf("result file %q absent from manifest", file.Path)
		}
		if file.Bytes != want.Bytes || file.SHA256 != want.SHA256 {
			return fmt.Errorf("result file %q identity does not match manifest", file.Path)
		}
	}
	return nil
}
