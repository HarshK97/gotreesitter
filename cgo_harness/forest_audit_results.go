package cgoharness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	forestAuditMaxArtifactBytes = int64(64 << 20)
	forestAuditMaxArtifacts     = 4096
)

type forestAuditArtifact struct {
	path   string
	data   []byte
	schema string
	digest string
}

func readForestAuditArtifact(filePath string) (forestAuditArtifact, error) {
	artifact := forestAuditArtifact{path: filePath}
	pathInfo, err := os.Lstat(filePath)
	if err != nil {
		return artifact, err
	}
	if !pathInfo.Mode().IsRegular() {
		return artifact, fmt.Errorf("forest audit artifact %s is not a regular file", filePath)
	}
	if pathInfo.Size() < 1 || pathInfo.Size() > forestAuditMaxArtifactBytes {
		return artifact, fmt.Errorf("forest audit artifact %s size %d outside 1..%d", filePath, pathInfo.Size(), forestAuditMaxArtifactBytes)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return artifact, err
	}
	defer file.Close()
	openInfo, err := file.Stat()
	if err != nil {
		return artifact, err
	}
	if !openInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openInfo) || openInfo.Size() != pathInfo.Size() ||
		!openInfo.ModTime().Equal(pathInfo.ModTime()) {
		return artifact, fmt.Errorf("forest audit artifact %s changed while opening", filePath)
	}
	data, err := io.ReadAll(io.LimitReader(file, forestAuditMaxArtifactBytes+1))
	if err != nil {
		return artifact, err
	}
	if int64(len(data)) != openInfo.Size() {
		return artifact, fmt.Errorf("forest audit artifact %s changed while reading", filePath)
	}
	afterInfo, err := os.Lstat(filePath)
	if err != nil || !afterInfo.Mode().IsRegular() || !os.SameFile(openInfo, afterInfo) ||
		afterInfo.Size() != openInfo.Size() || !afterInfo.ModTime().Equal(openInfo.ModTime()) {
		return artifact, fmt.Errorf("forest audit artifact %s changed after reading", filePath)
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return artifact, fmt.Errorf("decode forest audit schema for %s: %w", filePath, err)
	}
	if strings.TrimSpace(header.Schema) == "" {
		return artifact, fmt.Errorf("forest audit artifact %s has no schema", filePath)
	}
	artifact.data = data
	artifact.schema = header.Schema
	artifact.digest = fmt.Sprintf("%x", sha256.Sum256(data))
	return artifact, nil
}

func (artifact forestAuditArtifact) decode(value any) error {
	return decodeForestAuditJSON(artifact.data, value)
}

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
	Language                 string                          `json:"language"`
	Status                   string                          `json:"status"`
	Classification           string                          `json:"classification,omitempty"`
	CorrectnessRelation      string                          `json:"correctness_relation,omitempty"`
	ScreeningEligible        bool                            `json:"screening_eligible"`
	PromotionEligible        bool                            `json:"promotion_eligible"`
	RoutedSpeedup            float64                         `json:"routed_speedup,omitempty"`
	ScreenRoutedSpeedup      float64                         `json:"screen_routed_speedup,omitempty"`
	ConfirmationStatus       string                          `json:"confirmation_status,omitempty"`
	Confirmation             *ForestAuditConfirmationSummary `json:"confirmation,omitempty"`
	ConfirmationCohortSHA256 string                          `json:"confirmation_cohort_sha256,omitempty"`
	ThreeWay                 *ForestAuditThreeWayRelation    `json:"three_way,omitempty"`
	PromotionBlockers        []string                        `json:"promotion_blockers,omitempty"`
	Production               *ForestAuditResult              `json:"production,omitempty"`
	COracle                  *ForestAuditResult              `json:"c_oracle,omitempty"`
}

type ForestAuditReport struct {
	Schema                  string                      `json:"schema"`
	GotreesitterRevision    string                      `json:"gotreesitter_revision"`
	CorpusManifestSHA256    string                      `json:"corpus_manifest_sha256"`
	CorpusLockSHA256        string                      `json:"corpus_lock_sha256"`
	ConfirmationIndexSHA256 string                      `json:"confirmation_index_sha256,omitempty"`
	Status                  string                      `json:"status"`
	LanguagesExpected       int                         `json:"languages_expected"`
	LanguagesComplete       int                         `json:"languages_complete"`
	LanguagesNoCoverage     int                         `json:"languages_no_forest_coverage"`
	Languages               []ForestAuditLanguageReport `json:"languages"`
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

func PublishForestAuditResult(resultsRoot, stagedPath string) (string, error) {
	artifact, err := readForestAuditArtifact(stagedPath)
	if err != nil {
		return "", err
	}
	if artifact.schema != ForestAuditResultSchema {
		return "", fmt.Errorf("forest audit result schema %q", artifact.schema)
	}
	var result ForestAuditResult
	if err := artifact.decode(&result); err != nil {
		return "", err
	}
	if err := validateForestAuditResult(result); err != nil {
		return "", err
	}
	target := filepath.Join(resultsRoot, result.Mode, result.Language+".json")
	if err := copyForestAuditArtifactExclusive(target, artifact.data); err != nil {
		return "", err
	}
	return target, nil
}

func ReduceForestAuditResults(manifestPath, resultsRoot string) (ForestAuditReport, error) {
	return ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, "")
}

func ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, confirmationIndexPath string) (ForestAuditReport, error) {
	reduction, err := newForestAuditReduction(manifestPath)
	if err != nil {
		return ForestAuditReport{}, err
	}
	if err := reduction.loadResults(resultsRoot); err != nil {
		return ForestAuditReport{}, err
	}
	if confirmationIndexPath != "" {
		if err := reduction.loadConfirmationIndex(resultsRoot, confirmationIndexPath); err != nil {
			return ForestAuditReport{}, err
		}
	}
	if err := reduction.validateConfirmationEvidence(); err != nil {
		return ForestAuditReport{}, err
	}
	return reduction.report(), nil
}

type forestAuditReduction struct {
	manifestDigest          string
	manifest                ForestCorpusManifest
	languages               []string
	manifestFiles           map[string]map[string]ForestCorpusManifestFile
	results                 map[string]map[string]*ForestAuditResult
	confirmations           map[string]map[string]ForestAuditConfirmationTrial
	runConfigs              map[string]ForestAuditRunConfig
	confirmationCohorts     map[string]string
	confirmationIndexDigest string
}

func newForestAuditReduction(manifestPath string) (*forestAuditReduction, error) {
	artifact, err := readForestAuditArtifact(manifestPath)
	if err != nil {
		return nil, err
	}
	if artifact.schema != ForestCorpusManifestSchema {
		return nil, fmt.Errorf("forest corpus manifest schema %q, want %q", artifact.schema, ForestCorpusManifestSchema)
	}
	var manifest ForestCorpusManifest
	if err := artifact.decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode forest corpus manifest: %w", err)
	}
	if err := validateForestCorpusManifestMetadata(manifest); err != nil {
		return nil, err
	}
	reduction := &forestAuditReduction{
		manifestDigest:      artifact.digest,
		manifest:            manifest,
		manifestFiles:       make(map[string]map[string]ForestCorpusManifestFile),
		results:             make(map[string]map[string]*ForestAuditResult),
		confirmations:       make(map[string]map[string]ForestAuditConfirmationTrial),
		runConfigs:          make(map[string]ForestAuditRunConfig),
		confirmationCohorts: make(map[string]string),
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
	artifactCount := 0
	return filepath.WalkDir(resultsRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			rel, err := filepath.Rel(resultsRoot, filePath)
			if err != nil {
				return err
			}
			if filepath.ToSlash(rel) == "confirmation" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		artifactCount++
		if artifactCount > forestAuditMaxArtifacts {
			return fmt.Errorf("forest audit bundle exceeds %d JSON artifacts", forestAuditMaxArtifacts)
		}
		artifact, err := readForestAuditArtifact(filePath)
		if err != nil {
			return err
		}
		switch artifact.schema {
		case ForestAuditReportSchema, "forest-audit-report-v3", "forest-audit-report-v2":
			return nil
		case ForestAuditResultSchema:
			return reduction.admitResult(artifact, resultsRoot)
		default:
			return fmt.Errorf("unsupported forest audit schema %q in %s", artifact.schema, filePath)
		}
	})
}

func (reduction *forestAuditReduction) admitResult(artifact forestAuditArtifact, resultsRoot string) error {
	var result ForestAuditResult
	if err := artifact.decode(&result); err != nil {
		return fmt.Errorf("read result %s: %w", artifact.path, err)
	}
	if err := validateForestAuditResult(result); err != nil {
		return fmt.Errorf("validate result %s: %w", artifact.path, err)
	}
	if err := reduction.validateResultIdentity(result); err != nil {
		return fmt.Errorf("result %s: %w", artifact.path, err)
	}
	rel, err := filepath.Rel(resultsRoot, artifact.path)
	if err != nil {
		return err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || parts[0] != result.Mode || parts[1] != result.Language+".json" {
		return fmt.Errorf("result %s is outside %s/<language>.json layout", artifact.path, result.Mode)
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
		Schema:                  ForestAuditReportSchema,
		GotreesitterRevision:    reduction.manifest.GotreesitterRevision,
		CorpusManifestSHA256:    reduction.manifestDigest,
		CorpusLockSHA256:        reduction.manifest.CorpusLock.SHA256,
		ConfirmationIndexSHA256: reduction.confirmationIndexDigest,
		Status:                  "pass",
		LanguagesExpected:       len(reduction.languages),
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
	row.ConfirmationCohortSHA256 = reduction.confirmationCohorts[language]
	row.Production = reduction.results[language]["production"]
	row.COracle = reduction.results[language]["c_oracle"]
	row.PromotionBlockers = forestPromotionBlockers(row.Production, row.COracle)
	if row.Production != nil && row.Production.RoutedNanos > 0 {
		row.RoutedSpeedup = float64(row.Production.PeerNanos) / float64(row.Production.RoutedNanos)
		row.ScreenRoutedSpeedup = row.RoutedSpeedup
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
	row.CorrectnessRelation, row.ThreeWay = forestAuditCorrectnessRelation(row.Production, row.COracle)
	if row.CorrectnessRelation == forestAuditRelationOracleCorrectionReview {
		row.Status = "incomplete"
		row.ConfirmationStatus = forestAuditRelationOracleCorrectionReview
		row.PromotionBlockers = []string{forestAuditRelationOracleCorrectionReview}
		return row
	}
	row.Status = "pass"
	if row.Production.Status != "pass" || row.COracle.Status != "pass" || row.CorrectnessRelation != forestAuditRelationProductionIdentity {
		row.Status = "fail"
	}
	baseBlockers := forestPromotionBlockers(row.Production, row.COracle)
	row.ScreeningEligible = row.Status == "pass" && len(baseBlockers) == 0
	row.PromotionBlockers = baseBlockers
	if !row.ScreeningEligible {
		return row
	}
	confirmation := summarizeForestAuditConfirmations(reduction.confirmations[language])
	row.Confirmation = &confirmation
	row.ConfirmationStatus = confirmation.Status
	if confirmation.Status == forestAuditConfirmationConfirmed {
		row.PromotionEligible = true
		row.PromotionBlockers = nil
	} else {
		row.PromotionBlockers = append(row.PromotionBlockers, confirmation.Status)
		if confirmation.Status != forestAuditConfirmationNeutral {
			row.Status = "incomplete"
		}
	}
	return row
}

func forestAuditCorrectnessRelation(production, cOracle *ForestAuditResult) (string, *ForestAuditThreeWayRelation) {
	if production == nil || cOracle == nil {
		return "", nil
	}
	if production.FilesDiverged == 0 && production.FilesRoutedDiverged == 0 {
		return forestAuditRelationProductionIdentity, nil
	}
	paths, ok := forestPotentialOracleCorrectionPaths(production, cOracle)
	if ok {
		return forestAuditRelationOracleCorrectionReview, &ForestAuditThreeWayRelation{
			Classification:   forestAuditRelationOracleCorrectionReview,
			Paths:            paths,
			EvidenceComplete: false,
		}
	}
	return forestAuditRelationUnresolvedDivergence, &ForestAuditThreeWayRelation{
		Classification:   forestAuditRelationUnresolvedDivergence,
		EvidenceComplete: false,
	}
}

func forestPotentialOracleCorrectionPaths(production, cOracle *ForestAuditResult) ([]string, bool) {
	if production == nil || cOracle == nil || cOracle.Status != "pass" || production.FilesRoutedDiverged == 0 {
		return nil, false
	}
	oracleByPath := make(map[string]ForestAuditFileResult, len(cOracle.Files))
	for _, file := range cOracle.Files {
		oracleByPath[file.Path] = file
	}
	var paths []string
	for _, file := range production.Files {
		if file.RoutedDiff == "" && file.Diff == "" {
			continue
		}
		oracle, ok := oracleByPath[file.Path]
		if !ok || oracle.Disposition != "accepted" || oracle.Diff != "" ||
			file.RoutedProvenance != forestAuditRouteForestFastPath || !file.Routed.Accepted || !file.Forest.Accepted {
			return nil, false
		}
		paths = append(paths, file.Path)
	}
	if len(paths) == 0 {
		return nil, false
	}
	sort.Strings(paths)
	return paths, true
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
		if production.FilesRoutedForest == 0 {
			blockers = append(blockers, "no_routed_forest_coverage")
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

func ReadForestAuditReport(path string) (ForestAuditReport, error) {
	var report ForestAuditReport
	artifact, err := readForestAuditArtifact(path)
	if err != nil {
		return report, err
	}
	if err := artifact.decode(&report); err != nil {
		return report, err
	}
	if report.Schema != ForestAuditReportSchema {
		return report, fmt.Errorf("forest audit report schema %q, want %q", report.Schema, ForestAuditReportSchema)
	}
	return report, nil
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
