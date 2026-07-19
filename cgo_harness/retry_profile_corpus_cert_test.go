//go:build cgo && treesitter_c_parity && treesitter_c_perfscan && gts_workcount

package cgoharness

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

const retryProfileCertSchema = "gts-retry-profile-cert/v1"

type retryProfileCertAttempt struct {
	LogicalRung        string                       `json:"logical_rung"`
	OperationCause     string                       `json:"operation_cause"`
	StopReason         gotreesitter.ParseStopReason `json:"stop_reason"`
	RootHasError       bool                         `json:"root_has_error"`
	RootEndByte        uint32                       `json:"root_end_byte"`
	ResolvedMaxStacks  int                          `json:"resolved_max_stacks"`
	ResolvedRetryPass  bool                         `json:"resolved_retry_pass"`
	ResolvedMergeLimit int                          `json:"resolved_max_merge_per_key"`
}

type retryProfileCertParse struct {
	WallNanos  int64                        `json:"wall_nanos"`
	TotalAlloc uint64                       `json:"total_alloc_bytes"`
	Attempts   []retryProfileCertAttempt    `json:"attempts"`
	StopReason gotreesitter.ParseStopReason `json:"stop_reason"`
	RootEnd    uint32                       `json:"root_end_byte"`
	HasError   bool                         `json:"root_has_error"`
	DeepSHA256 string                       `json:"deep_tree_sha256"`
}

type retryProfileCertFile struct {
	Path                  string                `json:"path"`
	Bytes                 int                   `json:"bytes"`
	SourceSHA256          string                `json:"source_sha256"`
	Class                 string                `json:"class"`
	OracleDeepSHA256      string                `json:"oracle_deep_tree_sha256"`
	OracleParity          bool                  `json:"oracle_parity,omitempty"`
	BaselineOracleParity  bool                  `json:"baseline_oracle_parity"`
	CandidateOracleParity bool                  `json:"candidate_oracle_parity"`
	OracleStatus          string                `json:"oracle_status"`
	OracleDetail          string                `json:"oracle_detail,omitempty"`
	Baseline              retryProfileCertParse `json:"baseline"`
	Candidate             retryProfileCertParse `json:"candidate"`
}

type retryProfileCertTotals struct {
	Files               int    `json:"files"`
	Bytes               int64  `json:"bytes"`
	BaselineWallNanos   int64  `json:"baseline_wall_nanos"`
	CandidateWallNanos  int64  `json:"candidate_wall_nanos"`
	BaselineTotalAlloc  uint64 `json:"baseline_total_alloc_bytes"`
	CandidateTotalAlloc uint64 `json:"candidate_total_alloc_bytes"`
	BaselineAttempts    int    `json:"baseline_attempts"`
	CandidateAttempts   int    `json:"candidate_attempts"`
	OracleMatches       int    `json:"oracle_matches"`
	OracleMismatches    int    `json:"oracle_mismatches"`
	OracleCrashes       int    `json:"oracle_crashes"`
	OracleUnavailable   int    `json:"oracle_unavailable"`
}

type retryProfileCertFailure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type retryProfileCertManifest struct {
	Schema               string                 `json:"schema"`
	Status               string                 `json:"status"`
	ResumeKey            string                 `json:"resume_key"`
	GeneratedAt          string                 `json:"generated_at"`
	Language             string                 `json:"language"`
	BlobSHA256           string                 `json:"blob_sha256"`
	GitRevision          string                 `json:"git_revision"`
	CorpusRoot           string                 `json:"corpus_root"`
	CorpusLock           string                 `json:"corpus_lock"`
	CorpusLockSHA256     string                 `json:"corpus_lock_sha256"`
	CorpusManifestSHA256 string                 `json:"corpus_manifest_sha256"`
	Oracle               perfScanOracleIdentity `json:"oracle"`
	CandidateProfile     struct {
		SkipExternalScannerRepeat bool `json:"skip_external_scanner_repeat"`
	} `json:"candidate_profile"`
	Totals  retryProfileCertTotals   `json:"totals"`
	Clean   retryProfileCertTotals   `json:"clean"`
	Error   retryProfileCertTotals   `json:"error"`
	Files   []retryProfileCertFile   `json:"files"`
	Failure *retryProfileCertFailure `json:"failure,omitempty"`
}

type retryProfileCertJournalRecord struct {
	ResumeKey string               `json:"resume_key"`
	File      retryProfileCertFile `json:"file"`
}

type retryProfileCertJournal struct {
	file  *os.File
	prior map[string]retryProfileCertFile
	key   string
}

func TestRetryProfileCorpusCertification(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT")) != "1" {
		t.Skip("set GTS_RETRY_PROFILE_CERT=1 to certify a retry profile")
	}
	name := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_LANG"))
	if name == "" || strings.Contains(name, ",") {
		t.Fatal("GTS_RETRY_PROFILE_CERT_LANG must name exactly one language")
	}
	blob := grammars.BlobByName(name)
	if len(blob) == 0 {
		t.Fatalf("BlobByName(%s) returned no data", name)
	}
	candidate, err := grammars.LoadLanguage(name, blob)
	if err != nil {
		t.Fatalf("load candidate language: %v", err)
	}
	baseline, err := grammars.LoadLanguage(name, blob)
	if err != nil {
		t.Fatalf("load baseline language: %v", err)
	}
	baseline.ExternalScannerFullParseRetryPolicy = gotreesitter.ExternalScannerFullParseRetryDefault
	if candidate.ExternalScannerFullParseRetryPolicy != gotreesitter.ExternalScannerFullParseRetrySkipRepeat {
		t.Fatalf("candidate %s does not suppress the external-scanner repeat", name)
	}
	staticOracle, err := buildStaticCPerfOracle(name)
	if err != nil {
		t.Fatalf("build locked static C oracle: %v", err)
	}
	defer staticOracle.Close()

	root := realCorpusBenchmarkRootForTest(t)
	langRoot := realCorpusBenchmarkLanguageRoot(t, root, name)
	files := loadRealCorpusBenchmarkFiles(t, langRoot, realCorpusBenchmarkFileFiltersFor(t, name, root))
	lockPath := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_LOCK"))
	if lockPath == "" {
		t.Fatal("set GTS_REAL_CORPUS_BENCH_LOCK to the authenticated corpus lock")
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read corpus lock: %v", err)
	}
	blobSum := sha256.Sum256(blob)
	lockSum := sha256.Sum256(lockData)
	manifest := retryProfileCertManifest{
		Schema:           retryProfileCertSchema,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		Language:         name,
		BlobSHA256:       hex.EncodeToString(blobSum[:]),
		GitRevision:      retryProfileCertGitRevision(),
		CorpusRoot:       langRoot,
		CorpusLock:       lockPath,
		CorpusLockSHA256: hex.EncodeToString(lockSum[:]),
		Oracle:           staticOracle.identity,
		Files:            make([]retryProfileCertFile, 0, len(files)),
	}
	manifest.ResumeKey = retryProfileCertResumeKey(manifest)
	manifest.CandidateProfile.SkipExternalScannerRepeat = true
	journal, err := openRetryProfileCertJournal(manifest)
	if err != nil {
		t.Fatalf("open retry-profile journal: %v", err)
	}
	if journal != nil {
		defer journal.Close()
	}
	defer func() {
		if manifest.Status == "" {
			manifest.Status = "failed"
		}
		if err := retryProfileCertWriteManifest(manifest); err != nil {
			t.Errorf("write retry-profile receipt: %v", err)
		}
	}()
	corpusHash := sha256.New()

	for _, file := range files {
		rel, _ := filepath.Rel(langRoot, file.path)
		sourceSum := sha256.Sum256(file.source)
		_, _ = fmt.Fprintf(corpusHash, "%s\x00%d\x00%x\n", filepath.ToSlash(rel), len(file.source), sourceSum)
		manifest.CorpusManifestSHA256 = hex.EncodeToString(corpusHash.Sum(nil))
		if journal != nil {
			if prior, ok := journal.prior[filepath.ToSlash(rel)]; ok {
				if prior.SourceSHA256 != hex.EncodeToString(sourceSum[:]) {
					t.Fatalf("resume source drift for %s", rel)
				}
				if prior.OracleStatus == "matched" {
					prior.OracleParity = true
					prior.BaselineOracleParity = true
					prior.CandidateOracleParity = true
				}
				retryProfileCertAccumulate(&manifest, prior)
				continue
			}
		}

		baselineResult, baselineTree := retryProfileCertParseOne(t, baseline, file.source)
		candidateResult, candidateTree := retryProfileCertParseOne(t, candidate, file.source)
		oracleDigest, oracleStatus, oracleDetail := retryProfileCertStaticOracle(staticOracle, file.source)
		class := "clean"
		if candidateResult.HasError {
			class = "error"
		}
		row := retryProfileCertFile{
			Path:                  filepath.ToSlash(rel),
			Bytes:                 len(file.source),
			SourceSHA256:          hex.EncodeToString(sourceSum[:]),
			Class:                 class,
			OracleDeepSHA256:      oracleDigest,
			OracleParity:          oracleDigest != "" && candidateResult.DeepSHA256 == oracleDigest,
			BaselineOracleParity:  oracleDigest != "" && baselineResult.DeepSHA256 == oracleDigest,
			CandidateOracleParity: oracleDigest != "" && candidateResult.DeepSHA256 == oracleDigest,
			OracleStatus:          oracleStatus,
			OracleDetail:          oracleDetail,
			Baseline:              baselineResult,
			Candidate:             candidateResult,
		}
		if row.OracleStatus == "completed" {
			if row.CandidateOracleParity {
				row.OracleStatus = "matched"
			} else {
				row.OracleStatus = "mismatch"
			}
		}
		retryProfileCertAccumulate(&manifest, row)
		if journal != nil {
			if err := journal.Append(row); err != nil {
				baselineTree.Release()
				candidateTree.Release()
				t.Fatalf("append retry-profile journal: %v", err)
			}
		}
		baselineTree.Release()
		candidateTree.Release()
		if !retryProfileCertEquivalent(baselineResult, candidateResult) ||
			row.BaselineOracleParity != row.CandidateOracleParity {
			manifest.Failure = &retryProfileCertFailure{Path: row.Path, Reason: "baseline and candidate parse results differ"}
			t.Fatalf("%s mismatch baseline=%+v candidate=%+v", rel, baselineResult, candidateResult)
		}
	}
	if manifest.Totals.BaselineAttempts <= manifest.Totals.CandidateAttempts {
		manifest.Failure = &retryProfileCertFailure{Reason: "candidate did not eliminate retry attempts"}
		t.Fatalf("candidate did not eliminate retry attempts: baseline=%d candidate=%d", manifest.Totals.BaselineAttempts, manifest.Totals.CandidateAttempts)
	}
	if manifest.Totals.CandidateWallNanos*100 > manifest.Totals.BaselineWallNanos*98 {
		manifest.Failure = &retryProfileCertFailure{Reason: "candidate wall-time improvement is below 2%"}
		t.Fatalf("candidate wall-time improvement below 2%%: baseline=%s candidate=%s",
			time.Duration(manifest.Totals.BaselineWallNanos), time.Duration(manifest.Totals.CandidateWallNanos))
	}
	manifest.Status = "passed"
	t.Logf("retry profile certified: language=%s files=%d bytes=%d attempts=%d->%d wall=%s->%s oracle=%d_match/%d_mismatch/%d_crash/%d_unavailable manifest=%s",
		name, manifest.Totals.Files, manifest.Totals.Bytes,
		manifest.Totals.BaselineAttempts, manifest.Totals.CandidateAttempts,
		time.Duration(manifest.Totals.BaselineWallNanos), time.Duration(manifest.Totals.CandidateWallNanos),
		manifest.Totals.OracleMatches, manifest.Totals.OracleMismatches,
		manifest.Totals.OracleCrashes, manifest.Totals.OracleUnavailable,
		manifest.CorpusManifestSHA256)
}

func retryProfileCertEquivalent(baseline, candidate retryProfileCertParse) bool {
	return baseline.DeepSHA256 == candidate.DeepSHA256 &&
		baseline.StopReason == candidate.StopReason &&
		baseline.RootEnd == candidate.RootEnd &&
		baseline.HasError == candidate.HasError
}

func retryProfileCertAccumulate(manifest *retryProfileCertManifest, row retryProfileCertFile) {
	manifest.Files = append(manifest.Files, row)
	retryProfileCertAddTotals(&manifest.Totals, row)
	if row.Class == "error" {
		retryProfileCertAddTotals(&manifest.Error, row)
	} else {
		retryProfileCertAddTotals(&manifest.Clean, row)
	}
}

func retryProfileCertResumeKey(manifest retryProfileCertManifest) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		manifest.Schema,
		manifest.Language,
		manifest.BlobSHA256,
		manifest.GitRevision,
		manifest.CorpusLockSHA256,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func openRetryProfileCertJournal(manifest retryProfileCertManifest) (*retryProfileCertJournal, error) {
	out := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_OUT"))
	if out == "" {
		return nil, nil
	}
	path := out + ".files.jsonl"
	journal := &retryProfileCertJournal{key: manifest.ResumeKey, prior: make(map[string]retryProfileCertFile)}
	if strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_RESUME")) == "1" {
		input, err := os.Open(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if input != nil {
			scanner := bufio.NewScanner(input)
			scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
			for scanner.Scan() {
				var record retryProfileCertJournalRecord
				if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
					_ = input.Close()
					return nil, fmt.Errorf("decode %s: %w", path, err)
				}
				if record.ResumeKey != manifest.ResumeKey {
					_ = input.Close()
					return nil, fmt.Errorf("%s belongs to resume key %s, want %s", path, record.ResumeKey, manifest.ResumeKey)
				}
				journal.prior[record.File.Path] = record.File
			}
			if err := scanner.Err(); err != nil {
				_ = input.Close()
				return nil, err
			}
			if err := input.Close(); err != nil {
				return nil, err
			}
		}
	} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	journal.file = file
	return journal, nil
}

func (journal *retryProfileCertJournal) Append(row retryProfileCertFile) error {
	data, err := json.Marshal(retryProfileCertJournalRecord{ResumeKey: journal.key, File: row})
	if err != nil {
		return err
	}
	_, err = journal.file.Write(append(data, '\n'))
	return err
}

func (journal *retryProfileCertJournal) Close() error {
	if journal == nil || journal.file == nil {
		return nil
	}
	return journal.file.Close()
}

func retryProfileCertWriteManifest(manifest retryProfileCertManifest) error {
	out := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_OUT"))
	if out == "" {
		return nil
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0o644)
}

func retryProfileCertStaticOracle(oracle *staticCPerfOracle, source []byte) (digest, status, detail string) {
	digest, _, err := oracle.deepDigest(source, 10*time.Second)
	if err == nil {
		return digest, "completed", ""
	}
	status = staticCOracleErrorStatus(err)
	detail = err.Error()
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "sigabrt") || strings.Contains(lower, "signal: aborted") || strings.Contains(lower, "assertion") {
		status = "oracle_crash"
	}
	return "", status, detail
}

func realCorpusBenchmarkRootForTest(t testing.TB) string {
	t.Helper()
	if root := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_ROOT")); root != "" {
		return root
	}
	t.Fatal("set GTS_REAL_CORPUS_BENCH_ROOT")
	return ""
}

func retryProfileCertParseOne(t testing.TB, lang *gotreesitter.Language, source []byte) (retryProfileCertParse, *gotreesitter.Tree) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	gotreesitter.BeginDiagnosticWorkCount()
	start := time.Now()
	tree, err := gotreesitter.NewParser(lang).Parse(source)
	wall := time.Since(start)
	counts := gotreesitter.EndDiagnosticWorkCount()
	runtime.ReadMemStats(&after)
	if err != nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatalf("parse: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		t.Fatal("parse returned no tree")
	}
	inspection, err := benchfixtures.InspectGoTree(tree.RootNode(), lang)
	if err != nil {
		tree.Release()
		t.Fatalf("deep digest: %v", err)
	}
	rt := tree.ParseRuntime()
	result := retryProfileCertParse{
		WallNanos:  wall.Nanoseconds(),
		TotalAlloc: after.TotalAlloc - before.TotalAlloc,
		StopReason: rt.StopReason,
		RootEnd:    tree.RootNode().EndByte(),
		HasError:   tree.RootNode().HasError(),
		DeepSHA256: inspection.SHA256,
		Attempts:   make([]retryProfileCertAttempt, 0, len(counts.Attempts)),
	}
	for _, attempt := range counts.Attempts {
		result.Attempts = append(result.Attempts, retryProfileCertAttempt{
			LogicalRung:        attempt.LogicalRung,
			OperationCause:     attempt.OperationCause,
			StopReason:         attempt.StopReason,
			RootHasError:       attempt.RootHasError,
			RootEndByte:        attempt.RootEndByte,
			ResolvedMaxStacks:  attempt.ResolvedMaxStacks,
			ResolvedRetryPass:  attempt.ResolvedRetryPass,
			ResolvedMergeLimit: attempt.ResolvedMaxMergePerKey,
		})
	}
	return result, tree
}

func retryProfileCertAddTotals(totals *retryProfileCertTotals, row retryProfileCertFile) {
	totals.Files++
	totals.Bytes += int64(row.Bytes)
	totals.BaselineWallNanos += row.Baseline.WallNanos
	totals.CandidateWallNanos += row.Candidate.WallNanos
	totals.BaselineTotalAlloc += row.Baseline.TotalAlloc
	totals.CandidateTotalAlloc += row.Candidate.TotalAlloc
	totals.BaselineAttempts += len(row.Baseline.Attempts)
	totals.CandidateAttempts += len(row.Candidate.Attempts)
	switch row.OracleStatus {
	case "matched":
		totals.OracleMatches++
	case "mismatch":
		totals.OracleMismatches++
	case "oracle_crash":
		totals.OracleCrashes++
	default:
		totals.OracleUnavailable++
	}
}

func retryProfileCertGitRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return "unknown"
}
