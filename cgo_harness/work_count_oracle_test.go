//go:build cgo && treesitter_c_parity && treesitter_c_perfscan

package cgoharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

const (
	workCountEnableEnv     = "GTS_WORK_COUNT_ORACLE"
	workCountReceiptEnv    = "GTS_WORK_COUNT_RECEIPT"
	workCountFixtureID     = "query_compile"
	workCountGoChildSchema = "gts-work-count-go-child/v3"
	workCountCChildSchema  = "gts-work-count-c-child/v3"
	workCountContract      = "gts-work-count/v2"
	workCountReceiptSchema = "gts-work-count-receipt/v3"
	workCountTimeout       = 2 * time.Minute

	workCountCAdmissionChildEnv    = "GTS_WORK_COUNT_C_ADMISSION_CHILD"
	workCountCAdmissionSourceEnv   = "GTS_WORK_COUNT_C_ADMISSION_SOURCE"
	workCountCAdmissionResultEnv   = "GTS_WORK_COUNT_C_ADMISSION_RESULT"
	workCountCAdmissionChildSchema = "gts-work-count-c-admission-child/v1"
	workCountRepoChildEnv          = "GTS_WORK_COUNT_REPO_CHILD"
	workCountRepoLabelEnv          = "GTS_WORK_COUNT_REPO_LABEL"
	workCountRepoURLEnv            = "GTS_WORK_COUNT_REPO_URL"
	workCountRepoCommitEnv         = "GTS_WORK_COUNT_REPO_COMMIT"
	workCountRepoDestinationEnv    = "GTS_WORK_COUNT_REPO_DESTINATION"
	workCountRepoLanguageEnv       = "GTS_WORK_COUNT_REPO_LANGUAGE"
)

var workCountDirectFields = []string{
	"shifts", "reductions", "explicit_recover_actions",
	"reduction_pop_requests", "emitted_pop_paths", "emitted_pop_payloads",
	"selected_nodes", "selected_parent_nodes", "selected_leaf_nodes",
}

var workCountTerminalFields = []string{"accept_actions"}

var workCountProxyFields = []string{
	"table_lookups_proxy", "action_entries_examined_proxy",
	"lexer_front_door_calls_proxy", "stack_version_creations_proxy",
	"merge_attempts_proxy", "merge_successes_proxy",
	"graph_link_additions_proxy", "leaf_constructions_proxy",
	"parent_constructions_proxy", "pending_parent_constructions_proxy",
	"no_tree_parent_constructions_proxy",
}

type workCountCounters struct {
	Contract string `json:"contract"`
	Overflow bool   `json:"overflow"`
	workCountCounterValues
}

type workCountCounterValues struct {
	Shifts                 uint64 `json:"shifts"`
	Reductions             uint64 `json:"reductions"`
	AcceptActions          uint64 `json:"accept_actions"`
	ExplicitRecoverActions uint64 `json:"explicit_recover_actions"`
	ReductionPopRequests   uint64 `json:"reduction_pop_requests"`
	EmittedPopPaths        uint64 `json:"emitted_pop_paths"`
	EmittedPopPayloads     uint64 `json:"emitted_pop_payloads"`
	SelectedNodes          uint64 `json:"selected_nodes"`
	SelectedParentNodes    uint64 `json:"selected_parent_nodes"`
	SelectedLeafNodes      uint64 `json:"selected_leaf_nodes"`

	TableLookupsProxy               uint64 `json:"table_lookups_proxy"`
	ActionEntriesExaminedProxy      uint64 `json:"action_entries_examined_proxy"`
	LexerFrontDoorCallsProxy        uint64 `json:"lexer_front_door_calls_proxy"`
	StackVersionCreationsProxy      uint64 `json:"stack_version_creations_proxy"`
	MergeAttemptsProxy              uint64 `json:"merge_attempts_proxy"`
	MergeSuccessesProxy             uint64 `json:"merge_successes_proxy"`
	GraphLinkAdditionsProxy         uint64 `json:"graph_link_additions_proxy"`
	LeafConstructionsProxy          uint64 `json:"leaf_constructions_proxy"`
	ParentConstructionsProxy        uint64 `json:"parent_constructions_proxy"`
	PendingParentConstructionsProxy uint64 `json:"pending_parent_constructions_proxy"`
	NoTreeParentConstructionsProxy  uint64 `json:"no_tree_parent_constructions_proxy"`
}

type workCountAttempt struct {
	Index          uint32 `json:"index"`
	LogicalRung    string `json:"logical_rung"`
	OperationCause string `json:"operation_cause"`

	RequestedMaxStacks       int  `json:"requested_max_stacks"`
	RequestedMaxNodes        int  `json:"requested_max_nodes"`
	RequestedMaxMergePerKey  int  `json:"requested_max_merge_per_key"`
	CapsResolved             bool `json:"caps_resolved"`
	ResolvedMaxStacks        int  `json:"resolved_max_stacks"`
	ResolvedRetryPass        bool `json:"resolved_retry_pass"`
	ResolvedMaxMergePerKey   int  `json:"resolved_max_merge_per_key"`
	ResolvedStackCullTrigger int  `json:"resolved_stack_cull_trigger"`
	ResolvedMaxIterations    int  `json:"resolved_max_iterations"`
	ResolvedMaxNodes         int  `json:"resolved_max_nodes"`

	StopReason   string `json:"stop_reason"`
	RootHasError bool   `json:"root_has_error"`
	RootEndByte  uint32 `json:"root_end_byte"`

	EntryToCaps    workCountCounterValues `json:"entry_to_caps"`
	CapsToFinalize workCountCounterValues `json:"caps_to_finalize"`
	Finalize       workCountCounterValues `json:"finalize"`
	Counters       workCountCounterValues `json:"counters"`
}

type workCountGoCounters struct {
	workCountCounters
	Attempts       []workCountAttempt     `json:"attempts"`
	OutsideAttempt workCountCounterValues `json:"outside_attempt"`
}

type workCountGoChildResult struct {
	Schema            string `json:"schema"`
	Engine            string `json:"engine"`
	Fixture           string `json:"fixture"`
	SourceSHA256      string `json:"source_sha256"`
	SourceBytes       uint32 `json:"source_bytes"`
	GrammarCommit     string `json:"grammar_commit"`
	GrammarBlobSHA256 string `json:"grammar_blob_sha256"`
	DigestFormat      string `json:"digest_format"`
	DeepTreeSHA256    string `json:"deep_tree_sha256"`
	RootEndByte       uint32 `json:"root_end_byte"`
	RootHasError      bool   `json:"root_has_error"`
	MaxStacksSeen     int    `json:"max_stacks_seen"`
	MultiStackIters   int    `json:"multi_stack_iterations"`
	MultiStackTokens  uint64 `json:"multi_stack_tokens"`
}

type workCountTaggedChildResult struct {
	workCountGoChildResult
	Counters workCountGoCounters `json:"counters"`
}

type workCountCChildResult struct {
	Schema         string            `json:"schema"`
	Engine         string            `json:"engine"`
	DigestFormat   string            `json:"digest_format"`
	DeepTreeSHA256 string            `json:"deep_tree_sha256,omitempty"`
	SourceBytes    uint32            `json:"source_bytes"`
	RootEndByte    uint32            `json:"root_end_byte"`
	RootHasError   bool              `json:"root_has_error"`
	Counters       workCountCounters `json:"counters"`
}

type workCountCAdmissionChildResult struct {
	Schema         string `json:"schema"`
	DigestFormat   string `json:"digest_format"`
	DeepTreeSHA256 string `json:"deep_tree_sha256"`
	SourceSHA256   string `json:"source_sha256"`
}

type workCountToolIdentity struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type workCountArtifactIdentity struct {
	ArtifactSHA256    string                `json:"artifact_sha256"`
	SourceSnapshotSHA string                `json:"source_snapshot_sha256,omitempty"`
	Tool              workCountToolIdentity `json:"tool"`
	Flags             []string              `json:"flags"`
}

type workCountCIdentity struct {
	Artifact      workCountArtifactIdentity        `json:"artifact"`
	Linker        workCountToolIdentity            `json:"linker"`
	PatchTool     workCountToolIdentity            `json:"patch_tool"`
	SymbolTool    workCountToolIdentity            `json:"symbol_tool"`
	VerifierTools map[string]workCountToolIdentity `json:"verifier_tools"`
	LinkageProof  string                           `json:"linkage_proof"`
	RuntimeCommit string                           `json:"runtime_commit"`
	RuntimeTree   string                           `json:"runtime_source_tree_oid"`
	PatchSHA256   string                           `json:"patch_sha256"`
	DriverSHA256  string                           `json:"driver_sha256"`
	InputSHA256   string                           `json:"input_snapshot_sha256"`
	InputFiles    int                              `json:"input_snapshot_files"`
	Environment   map[string]string                `json:"environment"`
	Language      perfScanOracleLanguageIdentity   `json:"language"`
}

type workCountRatio struct {
	Field   string   `json:"field"`
	Class   string   `json:"class"`
	Go      uint64   `json:"go"`
	C       uint64   `json:"c"`
	GoOverC *float64 `json:"go_over_c,omitempty"`
}

type workCountReceipt struct {
	Schema         string                    `json:"schema"`
	Fixture        string                    `json:"fixture"`
	SourceSHA256   string                    `json:"source_sha256"`
	SourceBytes    int                       `json:"source_bytes"`
	DigestFormat   string                    `json:"digest_format"`
	DeepTreeSHA256 string                    `json:"deep_tree_sha256"`
	ManifestSHA256 string                    `json:"contract_manifest_sha256"`
	Source         workCountGitProvenance    `json:"source"`
	GoEnvironment  workCountEnvironment      `json:"go_environment"`
	GoAdmission    workCountArtifactIdentity `json:"go_admission_artifact"`
	GoArtifact     workCountArtifactIdentity `json:"go_work_count_artifact"`
	CArtifact      workCountCIdentity        `json:"static_c_artifact"`
	GoCounts       workCountGoCounters       `json:"go_counts"`
	CCounts        workCountCounters         `json:"static_c_counts"`
	Ratios         []workCountRatio          `json:"ratios"`
	GoSurplus      uint64                    `json:"go_construction_surplus"`
	CSurplus       uint64                    `json:"static_c_construction_surplus"`
}

type workCountCBuild struct {
	Artifact string
	Identity workCountCIdentity
	Cleanup  func()
	Recheck  func() error
}

func TestAuthenticatedWorkCountOracle(t *testing.T) {
	if os.Getenv(workCountEnableEnv) != "1" {
		t.Skip(workCountEnableEnv + "=1 is required")
	}
	repoRoot := workCountRepoRoot(t)
	receiptPath := workCountPrepareReceiptPath(t, repoRoot)
	tempRoot := t.TempDir()
	sourceSnapshot := workCountPrepareSourceSnapshot(t, repoRoot, tempRoot)
	// Worktree-routing variables are needed only to authenticate and snapshot
	// the mounted source. They must not leak into pinned runtime/grammar Git
	// operations performed later by the static-C builders.
	workCountClearAmbientGitRouting(t)
	fixture := workCountLoadFixture(t)
	manifestPath := filepath.Join(sourceSnapshot.Root, "cgo_harness", "work_count", "contract_v2.json")
	patchPath := filepath.Join(sourceSnapshot.Root, "cgo_harness", "work_count", "tree_sitter_v0_25_1.patch")
	driverPath := filepath.Join(sourceSnapshot.Root, "cgo_harness", "pure_c", "work_count_oracle.c")
	manifestSHA := workCountFileSHA(t, manifestPath)
	patchSHA := workCountFileSHA(t, patchPath)
	driverSHA := workCountFileSHA(t, driverPath)
	workCountValidateManifest(t, manifestPath)

	sourcePath := filepath.Join(tempRoot, "query_compile.go")
	if err := os.WriteFile(sourcePath, fixture.Source, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := workCountFileSHA(t, sourcePath); got != fixture.Fixture.SHA256 {
		t.Fatalf("source snapshot sha=%s want=%s", got, fixture.Fixture.SHA256)
	}

	// Admission deliberately precedes every instrumented build and run. An
	// ordinary untagged child built from the private source snapshot and the
	// unmodified static C oracle must reproduce the frozen digest first.
	goEnvironment := workCountChosenEnvironment()
	t.Log("build: ordinary untagged Go admission child")
	goAdmissionArtifact, goAdmissionIdentity, goAdmissionRecheck := workCountBuildGo(t, sourceSnapshot, tempRoot, goEnvironment, false)
	t.Log("admission: ordinary untagged Go child")
	uninstGo := workCountRunGoAdmission(t, goAdmissionArtifact, sourcePath, tempRoot, goEnvironment)
	workCountValidateGoChild(t, "ordinary Go admission", "go-production-glr-untagged", uninstGo, fixture)
	t.Log("admission: unmodified static C")
	uninstC := workCountUninstrumentedCAdmission(t, fixture, sourcePath, tempRoot)
	if uninstGo.DeepTreeSHA256 != uninstC || uninstGo.DeepTreeSHA256 != fixture.Fixture.DeepTreeSHA256 {
		t.Fatalf("uninstrumented admission mismatch: go=%s static_c=%s frozen=%s", uninstGo.DeepTreeSHA256, uninstC, fixture.Fixture.DeepTreeSHA256)
	}

	t.Log("build: instrumented static C child")
	cBuild := workCountBuildC(t, repoRoot, sourceSnapshot.Root, patchPath, driverPath)
	defer cBuild.Cleanup()
	t.Log("build: tagged diagnostic Go child")
	goArtifact, goIdentity, goRecheck := workCountBuildGo(t, sourceSnapshot, tempRoot, goEnvironment, true)

	t.Log("run: instrumented static C child")
	cResult := workCountRunC(t, cBuild.Artifact, sourcePath, tempRoot)
	t.Log("run: tagged diagnostic Go child")
	goResult := workCountRunGo(t, goArtifact, sourcePath, tempRoot, goEnvironment)
	workCountValidateCChild(t, "static C", "static-c-instrumented-glr", cResult, fixture, uninstC)
	workCountValidateGoChild(t, "tagged Go", "go-production-glr-tagged-diagnostic", goResult.workCountGoChildResult, fixture)
	workCountValidateGoCounters(t, "tagged Go", goResult.Counters, uint32(len(fixture.Source)))
	if cResult.DeepTreeSHA256 != goResult.DeepTreeSHA256 {
		t.Fatalf("instrumented deep digest mismatch: go=%s static_c=%s", goResult.DeepTreeSHA256, cResult.DeepTreeSHA256)
	}
	if err := cBuild.Recheck(); err != nil {
		t.Fatalf("static C identity drift: %v", err)
	}
	if err := goRecheck(); err != nil {
		t.Fatalf("Go identity drift: %v", err)
	}
	if err := goAdmissionRecheck(); err != nil {
		t.Fatalf("Go admission identity drift: %v", err)
	}
	if err := sourceSnapshot.Recheck(); err != nil {
		t.Fatalf("source provenance drift: %v", err)
	}
	if got := workCountFileSHA(t, sourcePath); got != fixture.Fixture.SHA256 {
		t.Fatalf("source snapshot identity drift: got %s want %s", got, fixture.Fixture.SHA256)
	}
	for label, pathAndSHA := range map[string][2]string{
		"manifest": {manifestPath, manifestSHA}, "patch": {patchPath, patchSHA}, "driver": {driverPath, driverSHA},
	} {
		if got := workCountFileSHA(t, pathAndSHA[0]); got != pathAndSHA[1] {
			t.Fatalf("%s identity drift: got %s want %s", label, got, pathAndSHA[1])
		}
	}

	receipt := workCountReceipt{
		Schema: workCountReceiptSchema, Fixture: fixture.Fixture.ID,
		SourceSHA256: fixture.Fixture.SHA256, SourceBytes: len(fixture.Source),
		DigestFormat: benchfixtures.DeepTreeDigestVersion, DeepTreeSHA256: uninstGo.DeepTreeSHA256,
		ManifestSHA256: manifestSHA, Source: sourceSnapshot.Provenance,
		GoEnvironment: goEnvironment, GoAdmission: goAdmissionIdentity,
		GoArtifact: goIdentity, CArtifact: cBuild.Identity,
		GoCounts: goResult.Counters, CCounts: cResult.Counters,
		Ratios:    workCountRatios(goResult.Counters.workCountCounters, cResult.Counters),
		GoSurplus: workCountConstructionSurplus(t, "Go", goResult.Counters.workCountCounters),
		CSurplus:  workCountConstructionSurplus(t, "static C", cResult.Counters),
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := workCountAtomicPublish(receiptPath, data); err != nil {
		t.Fatal(err)
	}
	if sourceSnapshot.Provenance.Authoritative {
		t.Logf("authenticated work-count receipt: %s", receiptPath)
	} else {
		t.Logf("non-authoritative dirty-source work-count receipt: %s", receiptPath)
	}
}

func workCountRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(wd)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repository root from %s: %v", wd, err)
	}
	return root
}

func workCountLoadFixture(t *testing.T) benchfixtures.LoadedFixture {
	t.Helper()
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if fixture.Fixture.ID == workCountFixtureID {
			return fixture
		}
	}
	t.Fatalf("fixture %s not found", workCountFixtureID)
	return benchfixtures.LoadedFixture{}
}

func workCountUninstrumentedCAdmission(t *testing.T, fixture benchfixtures.LoadedFixture, sourcePath, tempRoot string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(tempRoot, "static_c_admission.json")
	environment := workCountSanitizedEnv(os.Environ(), workCountCBuildEnvironment(), map[string]string{
		workCountCAdmissionChildEnv:  "1",
		workCountCAdmissionSourceEnv: sourcePath,
		workCountCAdmissionResultEnv: resultPath,
	})
	_, stderr, err := workCountRunCaptured(
		"",
		environment,
		workCountBuildTimeout+workCountTimeout+staticCPerfWallGrace,
		self,
		"-test.run", "^TestWorkCountStaticCAdmissionChild$",
		"-test.count=1",
	)
	if err != nil {
		t.Fatalf("bounded static C admission child: %v", err)
	}
	if len(stderr) != 0 {
		t.Fatalf("bounded static C admission child wrote stderr: %s", strings.TrimSpace(string(stderr)))
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result workCountCAdmissionChildResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Schema != workCountCAdmissionChildSchema || result.DigestFormat != benchfixtures.DeepTreeDigestVersion {
		t.Fatalf("static C admission child identity=%q/%q", result.Schema, result.DigestFormat)
	}
	if result.SourceSHA256 != fixture.Fixture.SHA256 {
		t.Fatalf("static C admission source sha=%s want=%s", result.SourceSHA256, fixture.Fixture.SHA256)
	}
	if err := fixture.Fixture.VerifyDeepTreeDigest(result.DeepTreeSHA256); err != nil {
		t.Fatal(err)
	}
	return result.DeepTreeSHA256
}

func TestWorkCountStaticCAdmissionChild(t *testing.T) {
	if os.Getenv(workCountCAdmissionChildEnv) != "1" {
		t.Skip("work-count static C admission child is not configured")
	}
	sourcePath := strings.TrimSpace(os.Getenv(workCountCAdmissionSourceEnv))
	resultPath := strings.TrimSpace(os.Getenv(workCountCAdmissionResultEnv))
	if sourcePath == "" || resultPath == "" {
		t.Fatal("work-count static C admission child paths are incomplete")
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := buildStaticCPerfOracle("go")
	if err != nil {
		t.Fatal(err)
	}
	defer oracle.Close()
	digest, sourceSHA, err := oracle.deepDigest(source, workCountTimeout)
	if err != nil {
		t.Fatal(err)
	}
	result := workCountCAdmissionChildResult{
		Schema: workCountCAdmissionChildSchema, DigestFormat: benchfixtures.DeepTreeDigestVersion,
		DeepTreeSHA256: digest, SourceSHA256: sourceSHA,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func workCountEnsurePinnedRepo(t *testing.T, label, repoURL, commit, destination, language string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	environment := workCountSanitizedEnv(os.Environ(), workCountCBuildEnvironment(), map[string]string{
		workCountRepoChildEnv:       "1",
		workCountRepoLabelEnv:       label,
		workCountRepoURLEnv:         repoURL,
		workCountRepoCommitEnv:      commit,
		workCountRepoDestinationEnv: destination,
		workCountRepoLanguageEnv:    language,
		"GIT_TERMINAL_PROMPT":       "0",
	})
	_, stderr, err := workCountRunCaptured(
		"",
		environment,
		workCountBuildTimeout,
		self,
		"-test.run", "^TestWorkCountEnsurePinnedRepoChild$",
		"-test.count=1",
	)
	if err != nil {
		t.Fatalf("bounded %s acquisition: %v", label, err)
	}
	if len(stderr) != 0 {
		t.Fatalf("bounded %s acquisition wrote stderr: %s", label, strings.TrimSpace(string(stderr)))
	}
	if err := workCountVerifyPinnedRepo(destination, commit, environment); err != nil {
		t.Fatalf("bounded %s verification: %v", label, err)
	}
}

func TestWorkCountEnsurePinnedRepoChild(t *testing.T) {
	if os.Getenv(workCountRepoChildEnv) != "1" {
		t.Skip("work-count pinned repository child is not configured")
	}
	label := strings.TrimSpace(os.Getenv(workCountRepoLabelEnv))
	repoURL := strings.TrimSpace(os.Getenv(workCountRepoURLEnv))
	commit := strings.TrimSpace(os.Getenv(workCountRepoCommitEnv))
	destination := strings.TrimSpace(os.Getenv(workCountRepoDestinationEnv))
	language := strings.TrimSpace(os.Getenv(workCountRepoLanguageEnv))
	if label == "" || repoURL == "" || commit == "" || destination == "" {
		t.Fatal("work-count pinned repository child configuration is incomplete")
	}
	if err := staticCEnsurePinnedRepo(label, repoURL, commit, destination, language); err != nil {
		t.Fatal(err)
	}
}

func workCountGitOutput(repoDir string, environment []string, args ...string) (string, error) {
	gitArgs := append([]string{"-c", "safe.directory=" + repoDir, "-C", repoDir}, args...)
	output, err := workCountRunBounded("", environment, workCountGitTimeout, "git", gitArgs...)
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func workCountVerifyPinnedRepo(repoDir, commit string, environment []string) error {
	head, err := workCountGitOutput(repoDir, environment, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("read HEAD: %w", err)
	}
	want, err := workCountGitOutput(repoDir, environment, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve pinned commit: %w", err)
	}
	if strings.TrimSpace(head) != strings.TrimSpace(want) {
		return fmt.Errorf("HEAD mismatch: got %s want %s", strings.TrimSpace(head), strings.TrimSpace(want))
	}
	status, err := workCountGitOutput(repoDir, environment, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return fmt.Errorf("inspect worktree: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("pinned worktree is dirty: %s", strings.TrimSpace(status))
	}
	return nil
}

func workCountResolveGoGrammarSources(entry parityLockEntry, pinnedDir string) (grammarDir, srcDir, parserPath, sourceMode string, cleanup func(), err error) {
	cleanup = func() {}
	srcDir = filepath.Join(pinnedDir, entry.Subdir)
	parserPath = filepath.Join(srcDir, "parser.c")
	info, err := os.Stat(parserPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", "", "", cleanup, fmt.Errorf("locked Go parser is unavailable at %s: %w", parserPath, err)
	}
	if version, ok := readParserLanguageVersion(parserPath); ok && (version < parityMinLanguageVersion || version > parityMaxLanguageVersion) {
		return "", "", "", "", cleanup, fmt.Errorf("locked Go parser ABI %d is outside %d..%d", version, parityMinLanguageVersion, parityMaxLanguageVersion)
	}
	return pinnedDir, srcDir, parserPath, staticCSourceLocked, cleanup, nil
}

func workCountLanguageIdentity(entry parityLockEntry, grammarDir, srcDir, parserPath string, scanners []string, symbol, sourceMode string, hasCXX bool, linker workCountToolIdentity) (perfScanOracleLanguageIdentity, error) {
	environment := workCountSanitizedEnv(os.Environ(), workCountCBuildEnvironment(), nil)
	tree, err := workCountGitOutput(grammarDir, environment, "rev-parse", entry.Commit+":"+filepath.ToSlash(entry.Subdir))
	if err != nil {
		return perfScanOracleLanguageIdentity{}, err
	}
	parserSHA, err := fileSHA256(parserPath)
	if err != nil {
		return perfScanOracleLanguageIdentity{}, err
	}
	identity := perfScanOracleLanguageIdentity{
		Language: entry.Name, GrammarRepo: entry.RepoURL, GrammarCommit: entry.Commit,
		GrammarSourceTree: strings.TrimSpace(tree), GrammarSourceMode: sourceMode,
		LanguageSymbol: symbol,
		Parser: perfScanOracleSourceIdentity{
			Path: staticCSourceRelative(srcDir, parserPath), SHA256: parserSHA, CompileFlags: staticCPerfGrammarCFlags,
		},
		LinkerPath: linker.Path, LinkerVersion: linker.Version, LinkerSHA256: linker.SHA256,
	}
	for _, scanner := range scanners {
		sha, err := fileSHA256(scanner)
		if err != nil {
			return perfScanOracleLanguageIdentity{}, err
		}
		flags := staticCPerfGrammarCFlags
		if ext := strings.ToLower(filepath.Ext(scanner)); ext == ".cc" || ext == ".cpp" || ext == ".cxx" {
			flags = staticCPerfGrammarCXXFlags
		}
		identity.Scanners = append(identity.Scanners, perfScanOracleSourceIdentity{
			Path: staticCSourceRelative(srcDir, scanner), SHA256: sha, CompileFlags: flags,
		})
	}
	if hasCXX && filepath.Base(linker.Path) != "c++" && filepath.Base(linker.Path) != "g++" {
		return perfScanOracleLanguageIdentity{}, fmt.Errorf("C++ grammar resolved non-C++ linker %s", linker.Path)
	}
	return identity, nil
}

func workCountBuildC(t *testing.T, repoRoot, sourceRoot, patchPath, driverPath string) workCountCBuild {
	t.Helper()
	lockPath := filepath.Join(sourceRoot, "grammars", "languages.lock")
	lock, err := loadParityLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := lock["go"]
	cacheRoot := strings.TrimSpace(os.Getenv(staticCPerfCacheEnv))
	if cacheRoot == "" {
		cacheRoot = filepath.Join(repoRoot, "harness_out", "c_oracle", "fleet_static")
	}
	runtimeDir := filepath.Join(cacheRoot, "sources", "tree-sitter-"+COracleRuntimeCommit)
	grammarDir := filepath.Join(cacheRoot, "sources", paritySafeName("go")+"-"+entry.Commit)
	workCountEnsurePinnedRepo(t, "runtime", staticCPerfRuntimeRepo, COracleRuntimeCommit, runtimeDir, "")
	workCountEnsurePinnedRepo(t, "go grammar", entry.RepoURL, entry.Commit, grammarDir, "go")
	resolvedGrammar, srcDir, parserPath, sourceMode, cleanupGrammar, err := workCountResolveGoGrammarSources(entry, grammarDir)
	if err != nil {
		t.Fatal(err)
	}
	scanners := staticCScannerSources(srcDir)
	hasCXX := false
	for _, scanner := range scanners {
		switch strings.ToLower(filepath.Ext(scanner)) {
		case ".cc", ".cpp", ".cxx":
			hasCXX = true
		}
	}
	compiler := workCountTool(t, "cc")
	linkerName := "cc"
	if hasCXX {
		linkerName = "c++"
	}
	linker := workCountTool(t, linkerName)
	patchTool := workCountTool(t, "git")
	symbolTool := workCountTool(t, "nm")
	cEnvironment := workCountCBuildEnvironment()
	commandEnvironment := workCountSanitizedEnv(os.Environ(), cEnvironment, nil)
	buildRoot, err := os.MkdirTemp("", "gts-work-count-c-*")
	if err != nil {
		cleanupGrammar()
		t.Fatal(err)
	}
	cleanup := func() {
		cleanupGrammar()
		_ = workCountMakeTreeWritable(filepath.Join(buildRoot, "inputs"))
		_ = os.RemoveAll(buildRoot)
	}

	// Copy every compiler input into a private snapshot before patching or
	// compiling. The pinned caches and outer worktree are never compiler input.
	inputRoot := filepath.Join(buildRoot, "inputs")
	privateRuntime := filepath.Join(inputRoot, "runtime")
	privateGrammar := filepath.Join(inputRoot, "grammar")
	privatePatch := filepath.Join(inputRoot, "tree_sitter.patch")
	privateDriver := filepath.Join(inputRoot, "work_count_oracle.c")
	privateLock := filepath.Join(inputRoot, "languages.lock")
	if err := workCountCopyTree(filepath.Join(runtimeDir, "lib"), filepath.Join(privateRuntime, "lib")); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := workCountCopyTree(srcDir, privateGrammar); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := workCountCopyFile(patchPath, privatePatch, 0o444); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := workCountCopyFile(driverPath, privateDriver, 0o444); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := workCountCopyFile(lockPath, privateLock, 0o444); err != nil {
		cleanup()
		t.Fatal(err)
	}
	inputSHA, inputFiles, err := workCountTreeSHA(inputRoot)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := workCountMakeTreeReadOnly(inputRoot); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if got, files, err := workCountTreeSHA(inputRoot); err != nil || got != inputSHA || files != inputFiles {
		cleanup()
		t.Fatalf("C input snapshot drift after sealing: sha=%s files=%d want=%s/%d err=%v", got, files, inputSHA, inputFiles, err)
	}
	parserRel := staticCSourceRelative(srcDir, parserPath)
	privateParser := filepath.Join(privateGrammar, filepath.FromSlash(parserRel))
	privateScanners := make([]string, 0, len(scanners))
	for _, scanner := range scanners {
		privateScanners = append(privateScanners, filepath.Join(privateGrammar, filepath.FromSlash(staticCSourceRelative(srcDir, scanner))))
	}

	patchedRuntime := filepath.Join(buildRoot, "patched-runtime")
	if err := workCountCopyTree(privateRuntime, patchedRuntime); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := workCountMakeTreeWritable(patchedRuntime); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if _, err := workCountRunBounded(patchedRuntime, commandEnvironment, workCountBuildTimeout, patchTool.Path, "apply", "--check", privatePatch); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if _, err := workCountRunBounded(patchedRuntime, commandEnvironment, workCountBuildTimeout, patchTool.Path, "apply", privatePatch); err != nil {
		cleanup()
		t.Fatal(err)
	}

	objects := filepath.Join(buildRoot, "objects")
	if err := os.MkdirAll(objects, 0o755); err != nil {
		cleanup()
		t.Fatal(err)
	}
	runtimeObj := filepath.Join(objects, "runtime.o")
	runtimeFlags := strings.Fields(staticCPerfRuntimeCFlags)
	runtimeArgs := append([]string{}, runtimeFlags...)
	runtimeArgs = append(runtimeArgs, "-I", filepath.Join(patchedRuntime, "lib", "include"), "-I", filepath.Join(patchedRuntime, "lib", "src"), "-c", filepath.Join(patchedRuntime, "lib", "src", "lib.c"), "-o", runtimeObj)
	if _, err := workCountRunBounded("", commandEnvironment, workCountBuildTimeout, compiler.Path, runtimeArgs...); err != nil {
		cleanup()
		t.Fatal(err)
	}
	var grammarObjects []string
	for i, source := range append([]string{privateParser}, privateScanners...) {
		obj := filepath.Join(objects, fmt.Sprintf("grammar_%d.o", i))
		tool := compiler.Path
		flags := strings.Fields(staticCPerfGrammarCFlags)
		ext := strings.ToLower(filepath.Ext(source))
		if ext == ".cc" || ext == ".cpp" || ext == ".cxx" {
			tool = linker.Path
			flags = strings.Fields(staticCPerfGrammarCXXFlags)
		}
		args := append([]string{}, flags...)
		args = append(args, "-I", privateGrammar, "-I", filepath.Join(patchedRuntime, "lib", "include"), "-c", source, "-o", obj)
		if _, err := workCountRunBounded("", commandEnvironment, workCountBuildTimeout, tool, args...); err != nil {
			cleanup()
			t.Fatal(err)
		}
		grammarObjects = append(grammarObjects, obj)
	}
	symbol := workCountDefinedLanguageSymbol(t, entry, symbolTool.Path, grammarObjects[0], commandEnvironment)
	languageIdentity, err := workCountLanguageIdentity(entry, resolvedGrammar, privateGrammar, privateParser, privateScanners, symbol, sourceMode, hasCXX, linker)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	driverObj := filepath.Join(objects, "driver.o")
	driverFlags := strings.Fields(staticCPerfDriverCFlags)
	driverArgs := append([]string{}, driverFlags...)
	driverArgs = append(driverArgs, "-DTS_LANG_FN="+symbol, "-I", filepath.Join(patchedRuntime, "lib", "include", "tree_sitter"), "-I", filepath.Join(patchedRuntime, "lib", "src"), "-c", privateDriver, "-o", driverObj)
	if _, err := workCountRunBounded("", commandEnvironment, workCountBuildTimeout, compiler.Path, driverArgs...); err != nil {
		cleanup()
		t.Fatal(err)
	}
	artifact := filepath.Join(buildRoot, "work_count_oracle")
	linkArgs := append(strings.Fields(staticCPerfLinkFlags), "-o", artifact, driverObj)
	linkArgs = append(linkArgs, grammarObjects...)
	linkArgs = append(linkArgs, runtimeObj)
	if _, err := workCountRunBounded("", commandEnvironment, workCountBuildTimeout, linker.Path, linkArgs...); err != nil {
		cleanup()
		t.Fatal(err)
	}
	artifactSHA := workCountFileSHA(t, artifact)
	linkageProof, verifierTools, err := workCountVerifyStaticArtifact(artifact, symbol, commandEnvironment)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	runtimeTree, err := workCountGitOutput(runtimeDir, commandEnvironment, "rev-parse", COracleRuntimeCommit+":lib/src")
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	buildKeyInput, err := json.Marshal(struct {
		RuntimeCommit string                         `json:"runtime_commit"`
		RuntimeTree   string                         `json:"runtime_tree"`
		PatchSHA256   string                         `json:"patch_sha256"`
		DriverSHA256  string                         `json:"driver_sha256"`
		Compiler      workCountToolIdentity          `json:"compiler"`
		Linker        workCountToolIdentity          `json:"linker"`
		SymbolTool    workCountToolIdentity          `json:"symbol_tool"`
		Language      perfScanOracleLanguageIdentity `json:"language"`
		Flags         []string                       `json:"flags"`
		Environment   map[string]string              `json:"environment"`
		InputSHA256   string                         `json:"input_snapshot_sha256"`
	}{
		RuntimeCommit: COracleRuntimeCommit, RuntimeTree: strings.TrimSpace(runtimeTree),
		PatchSHA256: workCountFileSHA(t, privatePatch), DriverSHA256: workCountFileSHA(t, privateDriver),
		Compiler: compiler, Linker: linker, SymbolTool: symbolTool, Language: languageIdentity,
		Flags: []string{
			"runtime:" + staticCPerfRuntimeCFlags,
			"grammar_c:" + staticCPerfGrammarCFlags,
			"grammar_cxx:" + staticCPerfGrammarCXXFlags,
			"driver:" + staticCPerfDriverCFlags,
			"link:" + staticCPerfLinkFlags,
		},
		Environment: cEnvironment, InputSHA256: inputSHA,
	})
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	buildKey := sha256.Sum256(buildKeyInput)
	languageIdentity.BuildKeySHA256 = hex.EncodeToString(buildKey[:])
	languageIdentity.ArtifactSHA256 = artifactSHA
	languageIdentity.ArtifactStatic = true
	languageIdentity.ArtifactLinkage = linkageProof
	identity := workCountCIdentity{
		Artifact: workCountArtifactIdentity{ArtifactSHA256: artifactSHA, Tool: compiler, Flags: []string{
			"runtime:" + staticCPerfRuntimeCFlags,
			"grammar_c:" + staticCPerfGrammarCFlags,
			"grammar_cxx:" + staticCPerfGrammarCXXFlags,
			"driver:" + staticCPerfDriverCFlags,
			"link:" + staticCPerfLinkFlags,
		}},
		Linker: linker, PatchTool: patchTool, SymbolTool: symbolTool, VerifierTools: verifierTools, LinkageProof: linkageProof,
		RuntimeCommit: COracleRuntimeCommit, RuntimeTree: strings.TrimSpace(runtimeTree),
		PatchSHA256: workCountFileSHA(t, privatePatch), DriverSHA256: workCountFileSHA(t, privateDriver),
		InputSHA256: inputSHA, InputFiles: inputFiles, Environment: cEnvironment,
		Language: languageIdentity,
	}
	recheck := func() error {
		if got, err := fileSHA256(artifact); err != nil || got != artifactSHA {
			return fmt.Errorf("artifact sha=%s want=%s err=%v", got, artifactSHA, err)
		}
		if got, files, err := workCountTreeSHA(inputRoot); err != nil || got != inputSHA || files != inputFiles {
			return fmt.Errorf("C input snapshot sha=%s files=%d want=%s/%d err=%v", got, files, inputSHA, inputFiles, err)
		}
		tools := map[string]workCountToolIdentity{"compiler": compiler, "linker": linker, "patch": patchTool, "symbol": symbolTool}
		for label, identity := range verifierTools {
			tools["verifier_"+label] = identity
		}
		for label, want := range tools {
			got, err := workCountToolAt(want.Path)
			if err != nil || got != want {
				return fmt.Errorf("%s identity=%+v want=%+v err=%v", label, got, want, err)
			}
		}
		return nil
	}
	return workCountCBuild{Artifact: artifact, Identity: identity, Cleanup: cleanup, Recheck: recheck}
}

func workCountDefinedLanguageSymbol(t *testing.T, entry parityLockEntry, symbolTool, object string, environment []string) string {
	t.Helper()
	output, err := workCountRunBounded("", environment, workCountBuildTimeout, symbolTool, "-g", "--defined-only", object)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		symbol := fields[len(fields)-1]
		if strings.HasPrefix(symbol, "tree_sitter_") {
			found[symbol] = true
		}
	}
	for _, candidate := range parityLanguageSymbols(entry) {
		if found[candidate] {
			if len(found) != 1 {
				t.Fatalf("compiled parser object has ambiguous language symbols: %v", found)
			}
			return candidate
		}
	}
	t.Fatalf("compiled parser object has no accepted language symbol: %v", found)
	return ""
}

func workCountVerifyStaticArtifact(artifact, symbol string, environment []string) (string, map[string]workCountToolIdentity, error) {
	if strings.TrimSpace(symbol) == "" {
		return "", nil, fmt.Errorf("language symbol is empty")
	}
	nm, err := workCountToolNamed("nm")
	if err != nil {
		return "", nil, err
	}
	readelf, err := workCountToolNamed("readelf")
	if err != nil {
		return "", nil, err
	}
	nmOutput, err := workCountRunBounded("", environment, workCountGitTimeout, nm.Path, artifact)
	if err != nil {
		return "", nil, fmt.Errorf("nm: %w", err)
	}
	defined := make(map[string]bool)
	for _, line := range strings.Split(string(nmOutput), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && !strings.EqualFold(fields[len(fields)-2], "U") {
			defined[fields[len(fields)-1]] = true
		}
	}
	for _, required := range []string{"ts_parser_parse", symbol} {
		if !defined[required] {
			return "", nil, fmt.Errorf("artifact does not define %s", required)
		}
	}
	dynamicOutput, err := workCountRunBounded("", environment, workCountGitTimeout, readelf.Path, "-d", artifact)
	if err != nil {
		return "", nil, fmt.Errorf("readelf dynamic-section proof: %w", err)
	}
	if bytes.Contains(dynamicOutput, []byte("NEEDED")) {
		return "", nil, fmt.Errorf("artifact has dynamic dependencies: %s", strings.TrimSpace(string(dynamicOutput)))
	}
	programOutput, err := workCountRunBounded("", environment, workCountGitTimeout, readelf.Path, "-l", artifact)
	if err != nil {
		return "", nil, fmt.Errorf("readelf program-header proof: %w", err)
	}
	if bytes.Contains(programOutput, []byte("INTERP")) {
		return "", nil, fmt.Errorf("artifact has a dynamic program interpreter: %s", strings.TrimSpace(string(programOutput)))
	}
	return staticCPerfLinkageProof, map[string]workCountToolIdentity{"nm": nm, "readelf": readelf}, nil
}

func workCountBuildGo(t *testing.T, source workCountSourceSnapshot, tempRoot string, environment workCountEnvironment, tagged bool) (string, workCountArtifactIdentity, func() error) {
	t.Helper()
	tool := workCountTool(t, "go")
	name := "go_admission.test"
	flags := []string{"test", "-c", "."}
	args := []string{"test", "-c"}
	if tagged {
		name = "go_work_count.test"
		flags = []string{"test", "-c", "-tags", "gts_workcount", "."}
		args = append(args, "-tags", "gts_workcount")
	}
	artifact := filepath.Join(tempRoot, name)
	args = append(args, "-o", artifact, ".")
	buildEnv := workCountSanitizedEnv(os.Environ(), environment.Build, nil)
	if _, err := workCountRunBounded(source.Root, buildEnv, workCountBuildTimeout, tool.Path, args...); err != nil {
		t.Fatal(err)
	}
	sha := workCountFileSHA(t, artifact)
	identity := workCountArtifactIdentity{
		ArtifactSHA256: sha, SourceSnapshotSHA: source.Provenance.SnapshotSHA256,
		Tool: tool, Flags: flags,
	}
	return artifact, identity, func() error {
		gotSHA, err := fileSHA256(artifact)
		if err != nil || gotSHA != sha {
			return fmt.Errorf("artifact sha=%s want=%s err=%v", gotSHA, sha, err)
		}
		gotTool, err := workCountToolAt(tool.Path)
		if err != nil || gotTool != tool {
			return fmt.Errorf("Go tool identity=%+v want=%+v err=%v", gotTool, tool, err)
		}
		return source.Recheck()
	}
}

func workCountRunC(t *testing.T, artifact, sourcePath, tempRoot string) workCountCChildResult {
	t.Helper()
	dumpPath := filepath.Join(tempRoot, "c.deep")
	environment := workCountSanitizedEnv(os.Environ(), workCountCBuildEnvironment(), nil)
	stdout, stderr, err := workCountRunCaptured("", environment, workCountTimeout+staticCPerfWallGrace, artifact, sourcePath, dumpPath, strconv.FormatInt(workCountTimeout.Microseconds(), 10))
	if err != nil {
		t.Fatalf("static C work-count child: %v", err)
	}
	if len(stderr) != 0 {
		t.Fatalf("static C work-count child wrote stderr: %s", strings.TrimSpace(string(stderr)))
	}
	result := workCountDecodeCChild(t, stdout)
	result.DeepTreeSHA256 = workCountFileSHA(t, dumpPath)
	return result
}

func workCountRunGoAdmission(t *testing.T, artifact, sourcePath, tempRoot string, environment workCountEnvironment) workCountGoChildResult {
	t.Helper()
	resultPath := filepath.Join(tempRoot, "go-admission.json")
	workCountRunGoChild(t, artifact, "^TestWorkCountAdmissionChild$", sourcePath, resultPath, environment)
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	return workCountDecodeGoChild(t, data)
}

func workCountRunGo(t *testing.T, artifact, sourcePath, tempRoot string, environment workCountEnvironment) workCountTaggedChildResult {
	t.Helper()
	resultPath := filepath.Join(tempRoot, "go-work-count.json")
	workCountRunGoChild(t, artifact, "^TestDiagnosticWorkCountChild$", sourcePath, resultPath, environment)
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	return workCountDecodeTaggedGoChild(t, data)
}

func workCountRunGoChild(t *testing.T, artifact, testPattern, sourcePath, resultPath string, environment workCountEnvironment) {
	t.Helper()
	if err := os.Remove(resultPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	runtimeEnv := workCountSanitizedEnv(os.Environ(), environment.Runtime, map[string]string{
		"GTS_WORK_COUNT_SOURCE": sourcePath,
		"GTS_WORK_COUNT_RESULT": resultPath,
	})
	stdout, stderr, err := workCountRunCaptured("", runtimeEnv, workCountTimeout+staticCPerfWallGrace, artifact, "-test.run", testPattern, "-test.count=1")
	if err != nil {
		t.Fatalf("Go child: %v: stdout=%s stderr=%s", err, strings.TrimSpace(string(stdout)), strings.TrimSpace(string(stderr)))
	}
	if len(stderr) != 0 {
		t.Fatalf("Go child wrote stderr: %s", strings.TrimSpace(string(stderr)))
	}
}

var workCountGoChildFields = []string{
	"schema", "engine", "fixture", "source_sha256", "source_bytes",
	"grammar_commit", "grammar_blob_sha256", "digest_format",
	"deep_tree_sha256", "root_end_byte", "root_has_error",
	"max_stacks_seen", "multi_stack_iterations", "multi_stack_tokens",
}

func workCountDecodeGoChild(t *testing.T, data []byte) workCountGoChildResult {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode child object: %v: %s", err, data)
	}
	workCountRequireKeys(t, "Go child", raw, workCountGoChildFields)
	var result workCountGoChildResult
	workCountDecodeExact(t, data, &result)
	return result
}

func workCountDecodeTaggedGoChild(t *testing.T, data []byte) workCountTaggedChildResult {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode child object: %v: %s", err, data)
	}
	fields := append(append([]string(nil), workCountGoChildFields...), "counters")
	workCountRequireKeys(t, "tagged Go child", raw, fields)
	workCountValidateGoCounterObject(t, raw["counters"])
	var result workCountTaggedChildResult
	workCountDecodeExact(t, data, &result)
	return result
}

func workCountDecodeCChild(t *testing.T, data []byte) workCountCChildResult {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode C child object: %v: %s", err, data)
	}
	workCountRequireKeys(t, "C child", raw, []string{"schema", "engine", "digest_format", "source_bytes", "root_end_byte", "root_has_error", "counters"})
	workCountValidateCounterObject(t, raw["counters"])
	var result workCountCChildResult
	workCountDecodeExact(t, data, &result)
	return result
}

func workCountValidateCounterObject(t *testing.T, data json.RawMessage) {
	t.Helper()
	var counterRaw map[string]json.RawMessage
	if err := json.Unmarshal(data, &counterRaw); err != nil {
		t.Fatal(err)
	}
	counterKeys := append([]string{"contract", "overflow"}, workCountDirectFields...)
	counterKeys = append(counterKeys, workCountTerminalFields...)
	counterKeys = append(counterKeys, workCountProxyFields...)
	workCountRequireKeys(t, "counters", counterRaw, counterKeys)
}

func workCountValidateGoCounterObject(t *testing.T, data json.RawMessage) {
	t.Helper()
	var counterRaw map[string]json.RawMessage
	if err := json.Unmarshal(data, &counterRaw); err != nil {
		t.Fatal(err)
	}
	counterKeys := append([]string{"contract", "overflow"}, workCountDirectFields...)
	counterKeys = append(counterKeys, workCountTerminalFields...)
	counterKeys = append(counterKeys, workCountProxyFields...)
	counterKeys = append(counterKeys, "attempts", "outside_attempt")
	workCountRequireKeys(t, "Go counters", counterRaw, counterKeys)
	workCountValidateCounterValuesObject(t, "Go outside-attempt counters", counterRaw["outside_attempt"])
	var attempts []json.RawMessage
	if err := json.Unmarshal(counterRaw["attempts"], &attempts); err != nil {
		t.Fatal(err)
	}
	for i, attemptRaw := range attempts {
		var attempt map[string]json.RawMessage
		if err := json.Unmarshal(attemptRaw, &attempt); err != nil {
			t.Fatal(err)
		}
		workCountRequireKeys(t, fmt.Sprintf("Go attempt %d", i+1), attempt, []string{
			"index", "logical_rung", "operation_cause",
			"requested_max_stacks", "requested_max_nodes", "requested_max_merge_per_key",
			"caps_resolved", "resolved_max_stacks", "resolved_retry_pass", "resolved_max_merge_per_key",
			"resolved_stack_cull_trigger", "resolved_max_iterations", "resolved_max_nodes",
			"stop_reason", "root_has_error", "root_end_byte",
			"entry_to_caps", "caps_to_finalize", "finalize", "counters",
		})
		for _, key := range []string{"entry_to_caps", "caps_to_finalize", "finalize", "counters"} {
			workCountValidateCounterValuesObject(t, fmt.Sprintf("Go attempt %d %s", i+1, key), attempt[key])
		}
	}
}

func workCountValidateCounterValuesObject(t *testing.T, label string, data json.RawMessage) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	fields := append(append([]string(nil), workCountDirectFields...), workCountTerminalFields...)
	fields = append(fields, workCountProxyFields...)
	workCountRequireKeys(t, label, raw, fields)
}

func workCountDecodeExact(t *testing.T, data []byte, result any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("child JSON has trailing value: %v", err)
	}
}

func workCountValidateGoChild(t *testing.T, label, expectedEngine string, result workCountGoChildResult, fixture benchfixtures.LoadedFixture) {
	t.Helper()
	if result.Schema != workCountGoChildSchema || result.DigestFormat != benchfixtures.DeepTreeDigestVersion {
		t.Fatalf("%s mixed contract: schema=%q digest=%q", label, result.Schema, result.DigestFormat)
	}
	if result.Engine != expectedEngine {
		t.Fatalf("%s engine=%q want=%q", label, result.Engine, expectedEngine)
	}
	if result.Fixture != fixture.Fixture.ID || result.SourceSHA256 != fixture.Fixture.SHA256 {
		t.Fatalf("%s fixture/source=%q/%s want=%q/%s", label, result.Fixture, result.SourceSHA256, fixture.Fixture.ID, fixture.Fixture.SHA256)
	}
	if result.GrammarCommit != benchfixtures.GoGrammarCommit || result.GrammarBlobSHA256 != benchfixtures.GoGrammarBlobSHA256 {
		t.Fatalf("%s grammar=%s/%s want=%s/%s", label, result.GrammarCommit, result.GrammarBlobSHA256, benchfixtures.GoGrammarCommit, benchfixtures.GoGrammarBlobSHA256)
	}
	if result.SourceBytes != uint32(len(fixture.Source)) || result.RootEndByte != uint32(len(fixture.Source)) || result.RootHasError {
		t.Fatalf("%s span/error admission: source=%d root_end=%d error=%v", label, result.SourceBytes, result.RootEndByte, result.RootHasError)
	}
	if result.DeepTreeSHA256 != fixture.Fixture.DeepTreeSHA256 {
		t.Fatalf("%s digest=%s want=%s", label, result.DeepTreeSHA256, fixture.Fixture.DeepTreeSHA256)
	}
	identity := fixture.Fixture.WorkloadIdentity
	if result.MaxStacksSeen < identity.MinMaxStacksSeen || result.MultiStackIters < identity.MinMultiStackIterations || result.MultiStackTokens < identity.MinMultiStackTokens {
		t.Fatalf("%s GLR identity stacks=%d iterations=%d tokens=%d", label, result.MaxStacksSeen, result.MultiStackIters, result.MultiStackTokens)
	}
}

func workCountValidateCChild(t *testing.T, label, expectedEngine string, result workCountCChildResult, fixture benchfixtures.LoadedFixture, digest string) {
	t.Helper()
	if result.Schema != workCountCChildSchema || result.DigestFormat != benchfixtures.DeepTreeDigestVersion {
		t.Fatalf("%s mixed contract: schema=%q digest=%q", label, result.Schema, result.DigestFormat)
	}
	if result.Engine != expectedEngine {
		t.Fatalf("%s engine=%q want=%q", label, result.Engine, expectedEngine)
	}
	if result.SourceBytes != uint32(len(fixture.Source)) || result.RootEndByte != uint32(len(fixture.Source)) || result.RootHasError {
		t.Fatalf("%s span/error admission: source=%d root_end=%d error=%v", label, result.SourceBytes, result.RootEndByte, result.RootHasError)
	}
	if result.DeepTreeSHA256 != digest {
		t.Fatalf("%s instrumented digest=%s want uninstrumented=%s", label, result.DeepTreeSHA256, digest)
	}
	workCountValidateCounters(t, label, result.Counters)
}

func workCountValidateCounters(t *testing.T, label string, c workCountCounters) {
	t.Helper()
	if c.Contract != workCountContract || c.Overflow {
		t.Fatalf("%s counters contract=%q overflow=%v", label, c.Contract, c.Overflow)
	}
	if c.SelectedNodes == 0 || c.SelectedNodes != c.SelectedParentNodes+c.SelectedLeafNodes {
		t.Fatalf("%s selected node census invalid: total=%d parent=%d leaf=%d", label, c.SelectedNodes, c.SelectedParentNodes, c.SelectedLeafNodes)
	}
	if c.Reductions != c.ReductionPopRequests {
		t.Fatalf("%s reduce/pop-request mismatch: reductions=%d requests=%d", label, c.Reductions, c.ReductionPopRequests)
	}
}

func workCountValidateGoCounters(t *testing.T, label string, c workCountGoCounters, sourceBytes uint32) {
	t.Helper()
	workCountValidateCounters(t, label, c.workCountCounters)
	if len(c.Attempts) != 1 {
		t.Fatalf("%s attempts=%d want=1", label, len(c.Attempts))
	}
	attempt := c.Attempts[0]
	if attempt.Index != 1 || attempt.LogicalRung != "initial_full" || attempt.OperationCause != "fresh_dfa_full_parse" {
		t.Fatalf("%s attempt identity=%d/%q/%q", label, attempt.Index, attempt.LogicalRung, attempt.OperationCause)
	}
	if !attempt.CapsResolved {
		t.Fatalf("%s attempt caps were not resolved", label)
	}
	if attempt.StopReason != string("accepted") || attempt.RootHasError || attempt.RootEndByte != sourceBytes {
		t.Fatalf("%s attempt result stop=%q error=%v end=%d want=%d", label, attempt.StopReason, attempt.RootHasError, attempt.RootEndByte, sourceBytes)
	}
	if c.AcceptActions != 3 || attempt.Counters.AcceptActions != 3 {
		t.Fatalf("%s accept actions aggregate=%d attempt=%d want=3", label, c.AcceptActions, attempt.Counters.AcceptActions)
	}
	workCountRequireValueSum(t, label+" attempt phases", attempt.Counters, attempt.EntryToCaps, attempt.CapsToFinalize, attempt.Finalize)
	parts := make([]workCountCounterValues, 0, len(c.Attempts)+1)
	for _, item := range c.Attempts {
		parts = append(parts, item.Counters)
	}
	parts = append(parts, c.OutsideAttempt)
	workCountRequireValueSum(t, label+" aggregate attribution", c.workCountCounterValues, parts...)
}

func workCountRequireValueSum(t *testing.T, label string, total workCountCounterValues, parts ...workCountCounterValues) {
	t.Helper()
	totalValues := workCountCounterValueMap(total)
	sums := make(map[string]uint64, len(totalValues))
	for _, part := range parts {
		for field, value := range workCountCounterValueMap(part) {
			if ^uint64(0)-sums[field] < value {
				t.Fatalf("%s field %s sum overflow", label, field)
			}
			sums[field] += value
		}
	}
	for field, want := range totalValues {
		if got := sums[field]; got != want {
			t.Fatalf("%s field %s sum=%d want=%d", label, field, got, want)
		}
	}
}

func workCountCounterValueMap(values workCountCounterValues) map[string]uint64 {
	data, _ := json.Marshal(values)
	var raw map[string]uint64
	_ = json.Unmarshal(data, &raw)
	return raw
}

func workCountRatios(goCounts, cCounts workCountCounters) []workCountRatio {
	goValues := workCountValues(goCounts)
	cValues := workCountValues(cCounts)
	var ratios []workCountRatio
	for _, classFields := range []struct {
		class  string
		fields []string
	}{{"direct", workCountDirectFields}, {"terminal", workCountTerminalFields}, {"proxy", workCountProxyFields}} {
		for _, field := range classFields.fields {
			ratio := workCountRatio{Field: field, Class: classFields.class, Go: goValues[field], C: cValues[field]}
			if ratio.C != 0 {
				value := float64(ratio.Go) / float64(ratio.C)
				ratio.GoOverC = &value
			}
			ratios = append(ratios, ratio)
		}
	}
	return ratios
}

func workCountValues(c workCountCounters) map[string]uint64 {
	data, _ := json.Marshal(c)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	values := make(map[string]uint64, len(raw))
	for key, value := range raw {
		var count uint64
		if json.Unmarshal(value, &count) == nil {
			values[key] = count
		}
	}
	return values
}

func workCountConstructionSurplus(t *testing.T, label string, c workCountCounters) uint64 {
	t.Helper()
	if ^uint64(0)-c.LeafConstructionsProxy < c.ParentConstructionsProxy {
		t.Fatalf("%s construction sum overflow", label)
	}
	constructed := c.LeafConstructionsProxy + c.ParentConstructionsProxy
	if constructed < c.SelectedNodes {
		t.Fatalf("%s constructed payloads=%d below selected nodes=%d", label, constructed, c.SelectedNodes)
	}
	return constructed - c.SelectedNodes
}

func workCountValidateManifest(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	workCountRequireKeys(t, "manifest", raw, []string{"schema", "counter_contract", "scope", "direct", "terminal", "proxy", "derived", "attempt_attribution", "convergence_frontier", "admission"})
	var manifest struct {
		Schema          string            `json:"schema"`
		CounterContract string            `json:"counter_contract"`
		Scope           string            `json:"scope"`
		Direct          map[string]string `json:"direct"`
		Terminal        map[string]string `json:"terminal"`
		Proxy           map[string]string `json:"proxy"`
		Derived         map[string]string `json:"derived"`
		Attempt         struct {
			LogicalRungs                     []string `json:"logical_rungs"`
			ResolvedRetryPassIsIndependent   bool     `json:"resolved_retry_pass_is_independent"`
			Phases                           []string `json:"phases"`
			RequireAggregateEqualsAttributed bool     `json:"require_aggregate_equals_attempts_plus_outside"`
			CanonicalAttempts                int      `json:"canonical_query_compile_attempts"`
			CanonicalAcceptActions           uint64   `json:"canonical_query_compile_accept_actions"`
			RetryWitness                     struct {
				Path          string   `json:"path"`
				Bytes         int      `json:"bytes"`
				SHA256        string   `json:"sha256"`
				ExpectedRungs []string `json:"expected_rungs"`
			} `json:"retry_witness"`
			StraightLRControl struct {
				Path              string `json:"path"`
				Bytes             int    `json:"bytes"`
				SHA256            string `json:"sha256"`
				ExpectedMaxStacks int    `json:"expected_max_stacks"`
			} `json:"straight_lr_control"`
		} `json:"attempt_attribution"`
		Convergence map[string]any `json:"convergence_frontier"`
		Admission   map[string]any `json:"admission"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "gts-work-count-contract/v2" || manifest.CounterContract != workCountContract || strings.TrimSpace(manifest.Scope) == "" {
		t.Fatalf("manifest contract mismatch: schema=%q counters=%q scope=%q", manifest.Schema, manifest.CounterContract, manifest.Scope)
	}
	workCountRequireStringKeys(t, "manifest direct", manifest.Direct, workCountDirectFields)
	workCountRequireStringKeys(t, "manifest terminal", manifest.Terminal, workCountTerminalFields)
	workCountRequireStringKeys(t, "manifest proxy", manifest.Proxy, workCountProxyFields)
	workCountRequireStringKeys(t, "manifest derived", manifest.Derived, []string{"construction_surplus"})
	var attemptRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["attempt_attribution"], &attemptRaw); err != nil {
		t.Fatal(err)
	}
	attempt := make(map[string]json.RawMessage, len(attemptRaw))
	for key := range attemptRaw {
		attempt[key] = nil
	}
	workCountRequireKeys(t, "manifest attempt attribution", attempt, []string{
		"logical_rungs", "resolved_retry_pass_is_independent", "phases",
		"require_aggregate_equals_attempts_plus_outside", "canonical_query_compile_attempts",
		"canonical_query_compile_accept_actions", "retry_witness", "straight_lr_control",
	})
	if got := strings.Join(manifest.Attempt.LogicalRungs, ","); got != "initial_full,initial_merge,clean_wide,clean_wide_merge,recovery_wide_or_node,secondary_node,final_merge" {
		t.Fatalf("manifest logical rungs=%q", got)
	}
	if got := strings.Join(manifest.Attempt.Phases, ","); got != "entry_to_caps,caps_to_finalize,finalize" {
		t.Fatalf("manifest attempt phases=%q", got)
	}
	if !manifest.Attempt.ResolvedRetryPassIsIndependent || !manifest.Attempt.RequireAggregateEqualsAttributed ||
		manifest.Attempt.CanonicalAttempts != 1 || manifest.Attempt.CanonicalAcceptActions != 3 {
		t.Fatalf("manifest attribution invariants are incomplete: %+v", manifest.Attempt)
	}
	if got := strings.Join(manifest.Attempt.RetryWitness.ExpectedRungs, ","); got != "initial_full,initial_merge" {
		t.Fatalf("manifest retry witness rungs=%q", got)
	}
	if manifest.Attempt.StraightLRControl.ExpectedMaxStacks != 1 {
		t.Fatalf("manifest straight-LR max stacks=%d want=1", manifest.Attempt.StraightLRControl.ExpectedMaxStacks)
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(path), "..", ".."))
	workCountValidateFrozenWitness(t, repoRoot, "retry", manifest.Attempt.RetryWitness.Path, manifest.Attempt.RetryWitness.Bytes, manifest.Attempt.RetryWitness.SHA256)
	workCountValidateFrozenWitness(t, repoRoot, "straight-LR", manifest.Attempt.StraightLRControl.Path, manifest.Attempt.StraightLRControl.Bytes, manifest.Attempt.StraightLRControl.SHA256)
	convergence := make(map[string]json.RawMessage, len(manifest.Convergence))
	for key := range manifest.Convergence {
		convergence[key] = nil
	}
	workCountRequireKeys(t, "manifest convergence frontier", convergence, []string{
		"status", "max_events", "record_first_reject_per_reason", "identity", "outcomes", "snapshots",
		"include_cumulative_direct_and_proxy_counts", "terminal_counters", "interpretation",
	})
	if status, _ := manifest.Convergence["status"].(string); status != "schema_reserved_not_implemented" {
		t.Fatalf("manifest convergence status=%q", status)
	}
	if maxEvents, _ := manifest.Convergence["max_events"].(float64); maxEvents != 256 {
		t.Fatalf("manifest convergence max events=%v", manifest.Convergence["max_events"])
	}
	admission := make(map[string]json.RawMessage, len(manifest.Admission))
	for key := range manifest.Admission {
		admission[key] = nil
	}
	workCountRequireKeys(t, "manifest admission", admission, []string{
		"digest_format", "fixture", "require_uninstrumented_go_static_c_digest_match_first",
		"require_instrumented_digest_match", "require_full_span", "require_clean_root",
		"require_no_overflow", "require_exact_fields", "require_ordinary_untagged_go_child_first",
		"require_clean_git_source_for_authoritative_receipt", "require_private_content_addressed_go_source_snapshot",
		"require_private_c_input_snapshot", "sanitize_go_and_parser_environment", "atomic_receipt_publish",
	})
}

func workCountValidateFrozenWitness(t *testing.T, repoRoot, label, relativePath string, wantBytes int, wantSHA string) {
	t.Helper()
	if relativePath == "" || filepath.IsAbs(relativePath) {
		t.Fatalf("manifest %s witness path=%q", label, relativePath)
	}
	path := filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(relativePath)))
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("manifest %s witness escapes repository: path=%q rel=%q err=%v", label, path, rel, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() != int64(wantBytes) {
		t.Fatalf("manifest %s witness mode/bytes=%s/%d want regular/%d", label, info.Mode(), info.Size(), wantBytes)
	}
	if got := workCountFileSHA(t, path); got != wantSHA {
		t.Fatalf("manifest %s witness sha256=%s want=%s", label, got, wantSHA)
	}
}

func workCountRequireKeys(t *testing.T, label string, values map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(values))
	for key := range values {
		got = append(got, key)
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%s fields=%v want=%v", label, got, want)
	}
}

func workCountRequireStringKeys(t *testing.T, label string, values map[string]string, want []string) {
	t.Helper()
	raw := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s field %s has empty definition", label, key)
		}
		raw[key] = nil
	}
	workCountRequireKeys(t, label, raw, want)
}

func workCountTool(t *testing.T, name string) workCountToolIdentity {
	t.Helper()
	identity, err := workCountToolNamed(name)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func workCountToolNamed(name string) (workCountToolIdentity, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return workCountToolIdentity{}, err
	}
	return workCountToolAt(path)
}

func workCountToolAt(path string) (workCountToolIdentity, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return workCountToolIdentity{}, err
	}
	args := []string{"--version"}
	if filepath.Base(resolved) == "go" || filepath.Base(resolved) == "go.exe" {
		args = []string{"version"}
	}
	environment := workCountSanitizedEnv(os.Environ(), workCountCBuildEnvironment(), nil)
	out, err := workCountRunBounded("", environment, workCountGitTimeout, resolved, args...)
	if err != nil {
		return workCountToolIdentity{}, fmt.Errorf("read tool version %s: %w", resolved, err)
	}
	version := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if version == "" {
		return workCountToolIdentity{}, fmt.Errorf("tool %s returned an empty version", resolved)
	}
	sha, err := fileSHA256(resolved)
	if err != nil {
		return workCountToolIdentity{}, err
	}
	return workCountToolIdentity{Path: resolved, Version: version, SHA256: sha}, nil
}

func workCountFileSHA(t *testing.T, path string) string {
	t.Helper()
	sha, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func workCountCopyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		return closeErr
	})
}
