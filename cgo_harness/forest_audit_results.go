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

const (
	forestAuditRouteNotRun             = "not_run"
	forestAuditRouteForestFastPath     = "forest_fast_path"
	forestAuditRouteProductionFallback = "production_fallback"
	forestAuditClassNoForestCoverage   = "no_forest_coverage"
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
	Path             string             `json:"path"`
	Bytes            int64              `json:"bytes"`
	SHA256           string             `json:"sha256"`
	Forest           ForestAuditOutcome `json:"forest"`
	Peer             ForestAuditOutcome `json:"peer"`
	Routed           ForestAuditOutcome `json:"routed"`
	RoutedProvenance string             `json:"routed_provenance"`
	RoutedNanos      int64              `json:"routed_nanos"`
	RoutedDiff       string             `json:"routed_diff,omitempty"`
	Disposition      string             `json:"disposition"`
	Diff             string             `json:"diff,omitempty"`
	Decline          string             `json:"decline,omitempty"`
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
	FilesRoutedDiverged  int                     `json:"files_routed_diverged"`
	FilesRoutedForest    int                     `json:"files_routed_forest"`
	FilesRoutedFallback  int                     `json:"files_routed_fallback"`
	ForestNanos          int64                   `json:"forest_nanos"`
	PeerNanos            int64                   `json:"peer_nanos"`
	RoutedNanos          int64                   `json:"routed_nanos"`
	RoutedImproved       bool                    `json:"routed_improved"`
	Files                []ForestAuditFileResult `json:"files"`
}

type ForestAuditLanguageReport struct {
	Language          string             `json:"language"`
	Status            string             `json:"status"`
	Classification    string             `json:"classification,omitempty"`
	PromotionEligible bool               `json:"promotion_eligible"`
	RoutedSpeedup     float64            `json:"routed_speedup,omitempty"`
	PromotionBlockers []string           `json:"promotion_blockers,omitempty"`
	Production        *ForestAuditResult `json:"production,omitempty"`
	COracle           *ForestAuditResult `json:"c_oracle,omitempty"`
}

type ForestAuditReport struct {
	Schema               string                      `json:"schema"`
	GotreesitterRevision string                      `json:"gotreesitter_revision"`
	CorpusManifestSHA256 string                      `json:"corpus_manifest_sha256"`
	CorpusLockSHA256     string                      `json:"corpus_lock_sha256"`
	Status               string                      `json:"status"`
	LanguagesExpected    int                         `json:"languages_expected"`
	LanguagesComplete    int                         `json:"languages_complete"`
	LanguagesNoCoverage  int                         `json:"languages_no_forest_coverage"`
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
	return header.Schema == ForestAuditReportSchema || header.Schema == "forest-audit-report-v2", nil
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
		if row.Classification == forestAuditClassNoForestCoverage {
			report.LanguagesNoCoverage++
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
	row.PromotionBlockers = forestPromotionBlockers(row.Production, row.COracle)
	if row.Production != nil && row.Production.RoutedNanos > 0 {
		row.RoutedSpeedup = float64(row.Production.PeerNanos) / float64(row.Production.RoutedNanos)
	}
	if row.Production == nil && forestCOracleProvesNoCoverage(row.COracle) {
		row.Status = "pass"
		row.Classification = forestAuditClassNoForestCoverage
		row.PromotionBlockers = []string{forestAuditClassNoForestCoverage}
		return row
	}
	if row.Production == nil || row.COracle == nil {
		return row
	}
	row.Status = "pass"
	if row.Production.Status != "pass" || row.COracle.Status != "pass" {
		row.Status = "fail"
	}
	row.PromotionEligible = row.Status == "pass" && len(row.PromotionBlockers) == 0
	return row
}

func forestPromotionBlockers(production, cOracle *ForestAuditResult) []string {
	var blockers []string
	if production == nil {
		blockers = append(blockers, "missing_production")
	} else {
		if production.FilesDiverged > 0 {
			blockers = append(blockers, "forest_divergence")
		}
		if production.FilesRoutedDiverged > 0 {
			blockers = append(blockers, "routed_divergence")
		}
		if !production.RoutedImproved {
			blockers = append(blockers, "routed_not_faster")
		}
		if production.Status != "pass" && len(blockers) == 0 {
			blockers = append(blockers, "production_audit_failed")
		}
	}
	if cOracle == nil {
		blockers = append(blockers, "missing_c_oracle")
	} else {
		if forestCOracleHasTimeout(cOracle) {
			blockers = append(blockers, "c_oracle_timeout")
		}
		if cOracle.Status != "pass" {
			blockers = append(blockers, "c_oracle_failed")
		}
	}
	return blockers
}

// forestCOracleProvesNoCoverage recognizes a completed C-first screening lane
// that found no file on which the forest returned a candidate tree. This is a
// terminal ineligibility result for the authenticated corpus, not a correctness
// or performance promotion: no production lane is needed (or worth risking) to
// prove that an automatic forest route would have no fast-path coverage.
func forestCOracleProvesNoCoverage(result *ForestAuditResult) bool {
	if result == nil || result.Mode != "c_oracle" || result.FilesTotal == 0 ||
		result.FilesAccepted != 0 || result.FilesDiverged != 0 ||
		result.FilesDeclined != result.FilesTotal || forestCOracleHasTimeout(result) {
		return false
	}
	for _, file := range result.Files {
		if file.Disposition != "declined" || strings.TrimSpace(file.Decline) == "" {
			return false
		}
	}
	return true
}

func forestCOracleHasTimeout(result *ForestAuditResult) bool {
	if result == nil || result.Mode != "c_oracle" {
		return false
	}
	for _, file := range result.Files {
		if file.Disposition == "declined" && file.Decline == "timeout" {
			return true
		}
	}
	return false
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
	counts, err := validateForestAuditResultFiles(result.Mode, result.Files)
	if err != nil {
		return err
	}
	if result.FilesTotal != len(result.Files) || result.FilesAccepted != counts.accepted ||
		result.FilesDeclined != counts.declined || result.FilesDiverged != counts.diverged ||
		result.FilesRoutedDiverged != counts.routedDiverged || result.RoutedNanos != counts.routedNanos ||
		result.FilesRoutedForest != counts.routedForest || result.FilesRoutedFallback != counts.routedFallback ||
		counts.accepted+counts.declined != result.FilesTotal {
		return fmt.Errorf("incoherent forest audit file counts")
	}
	if err := validateForestAuditModeEvidence(result); err != nil {
		return err
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
	accepted       int
	declined       int
	diverged       int
	routedDiverged int
	routedForest   int
	routedFallback int
	routedNanos    int64
}

func validateForestAuditResultFiles(mode string, files []ForestAuditFileResult) (forestAuditResultCounts, error) {
	var counts forestAuditResultCounts
	seen := make(map[string]bool, len(files))
	previous := ""
	for i, file := range files {
		if err := validateForestAuditResultFile(mode, file, i, previous, seen); err != nil {
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
		counts.routedNanos += file.RoutedNanos
		if file.RoutedDiff != "" {
			counts.routedDiverged++
		}
		switch file.RoutedProvenance {
		case forestAuditRouteForestFastPath:
			counts.routedForest++
		case forestAuditRouteProductionFallback:
			counts.routedFallback++
		}
	}
	return counts, nil
}

func validateForestAuditResultFile(mode string, file ForestAuditFileResult, index int, previous string, seen map[string]bool) error {
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
	if file.RoutedNanos < 0 {
		return fmt.Errorf("forest audit result file %d has negative routed time", index)
	}
	if _, err := forestManifestDigest("result file", file.SHA256); err != nil {
		return fmt.Errorf("forest audit result file %d: %w", index, err)
	}
	if err := validateForestAuditDisposition(file, index); err != nil {
		return err
	}
	return validateForestAuditOutcomes(mode, file, index)
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

func validateForestAuditOutcomes(mode string, file ForestAuditFileResult, index int) error {
	if file.Bytes <= math.MaxUint32 {
		want := uint32(file.Bytes)
		if file.Forest.SourceLen != want || file.Forest.ExpectedEOF != want {
			return fmt.Errorf("forest audit result file %d forest source length does not match authenticated bytes", index)
		}
		if file.Peer.SourceLen != want || file.Peer.ExpectedEOF != want {
			return fmt.Errorf("forest audit result file %d peer source length does not match authenticated bytes", index)
		}
		if file.Routed.SourceLen != want || file.Routed.ExpectedEOF != want {
			return fmt.Errorf("forest audit result file %d routed source length does not match authenticated bytes", index)
		}
	}
	if err := validateForestAuditOutcomeCoherence(file.Forest, "forest", index); err != nil {
		return err
	}
	if err := validateForestAuditOutcomeCoherence(file.Peer, "peer", index); err != nil {
		return err
	}
	if err := validateForestAuditOutcomeCoherence(file.Routed, "routed", index); err != nil {
		return err
	}
	if err := validateForestAuditRoutedEvidence(mode, file, index); err != nil {
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

func validateForestAuditRoutedEvidence(mode string, file ForestAuditFileResult, index int) error {
	if mode == "c_oracle" {
		if file.Routed.Present || file.Routed.Accepted || file.Routed.StopReason != "not_run" ||
			file.RoutedProvenance != forestAuditRouteNotRun || file.RoutedNanos != 0 || file.RoutedDiff != "" {
			return fmt.Errorf("forest audit result file %d C-oracle routed evidence must be not_run", index)
		}
		return nil
	}
	if file.Routed.StopReason == "not_run" || file.RoutedNanos == 0 {
		return fmt.Errorf("forest audit result file %d production audit lacks routed evidence", index)
	}
	wantProvenance := forestAuditRouteProductionFallback
	if file.Routed.ForestFastPath {
		wantProvenance = forestAuditRouteForestFastPath
	}
	if file.RoutedProvenance != wantProvenance {
		return fmt.Errorf("forest audit result file %d routed provenance %q does not match forest-fast-path=%t", index, file.RoutedProvenance, file.Routed.ForestFastPath)
	}
	if file.RoutedDiff != "" {
		return nil
	}
	if file.RoutedProvenance == forestAuditRouteForestFastPath {
		return validateForestRoutedAcceptedOutcome(file.Routed, index)
	}
	if file.Routed != file.Peer {
		return fmt.Errorf("forest audit result file %d routed fallback outcome does not match production", index)
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

func validateForestRoutedAcceptedOutcome(outcome ForestAuditOutcome, index int) error {
	if !outcome.Present || !outcome.Accepted || !outcome.FullSpan || !outcome.LastTokenEOF ||
		outcome.RootHasError || outcome.Truncated || outcome.StoppedEarly {
		return fmt.Errorf("forest audit result file %d routed parse is not a complete accepted tree", index)
	}
	return nil
}

func validateForestAuditModeEvidence(result ForestAuditResult) error {
	if result.Mode == "c_oracle" {
		if result.FilesRoutedDiverged != 0 || result.FilesRoutedForest != 0 || result.FilesRoutedFallback != 0 ||
			result.RoutedNanos != 0 || result.RoutedImproved {
			return fmt.Errorf("C-oracle result carries routed aggregate evidence")
		}
	} else {
		if result.FilesRoutedForest+result.FilesRoutedFallback != result.FilesTotal {
			return fmt.Errorf("production routed provenance does not cover every file")
		}
		improved := result.PeerNanos > 0 && result.RoutedNanos < result.PeerNanos
		if result.RoutedImproved != improved {
			return fmt.Errorf("production routed improvement does not match aggregate timing")
		}
	}
	failed := result.FilesDiverged > 0 || result.FilesAccepted == 0
	if result.Mode == "production" {
		failed = failed || result.FilesRoutedDiverged > 0
	}
	if failed && result.Status != "fail" {
		return fmt.Errorf("forest audit result status must fail its correctness gates")
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
