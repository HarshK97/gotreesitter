package cgoharness

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestForestAuditRunPlanValidation(t *testing.T) {
	t.Setenv("GTS_FOREST_AUDIT_EXECUTION_ORDER", ForestAuditOrderRoutedFirst)
	t.Setenv("GTS_FOREST_AUDIT_REPEAT_COUNT", "7")
	plan, err := ForestAuditRunPlanFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if plan.ExecutionOrder != ForestAuditOrderRoutedFirst || plan.RepeatCount != 7 {
		t.Fatalf("plan = %#v", plan)
	}

	for name, plan := range map[string]ForestAuditRunPlan{
		"bad order":   {ExecutionOrder: "alternating", RepeatCount: 1},
		"zero repeat": {ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 0},
		"too many":    {ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: forestAuditConfirmationMaxRepeats + 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateForestAuditRunPlan(plan); err == nil {
				t.Fatalf("accepted invalid plan %#v", plan)
			}
		})
	}
}

func TestForestAuditRunConfigRequiresSingleCPUAndHostFingerprint(t *testing.T) {
	valid := ForestAuditRunConfig{
		Schema: ForestAuditRunConfigSchema, HostLabel: "quiet-box", HostFingerprint: strings.Repeat("b", 64),
		ImageID: "sha256:" + strings.Repeat("a", 64), Memory: "4g", CPUs: "1", CPUSetCPUs: "7",
		PIDs: "4096", GoMemLimit: "3GiB", GOMAXPROCS: 1, TestTimeout: "10m",
	}
	if err := validateForestAuditRunConfig(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ForestAuditRunConfig){
		"multiple CPUs":  func(config *ForestAuditRunConfig) { config.CPUs = "4" },
		"CPU range":      func(config *ForestAuditRunConfig) { config.CPUSetCPUs = "7-8" },
		"CPU list":       func(config *ForestAuditRunConfig) { config.CPUSetCPUs = "7,8" },
		"no fingerprint": func(config *ForestAuditRunConfig) { config.HostFingerprint = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := validateForestAuditRunConfig(candidate); err == nil {
				t.Fatalf("accepted invalid run config %#v", candidate)
			}
		})
	}
}

func TestForestAuditConfirmationTrialValidatesAuthenticatedIdentity(t *testing.T) {
	result := forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000)
	plan := ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1}
	trial, err := NewForestAuditConfirmationTrial(forestAuditConfirmationPairA, strings.Repeat("d", 64), "", plan, result)
	if err != nil {
		t.Fatal(err)
	}
	if trial.Schema != ForestAuditConfirmationTrialSchema || trial.Result.Schema != "forest-audit-result-v2" {
		t.Fatalf("trial = %#v", trial)
	}

	for name, mutate := range map[string]func(*ForestAuditConfirmationTrial){
		"order mismatch":         func(trial *ForestAuditConfirmationTrial) { trial.ExecutionOrder = ForestAuditOrderRoutedFirst },
		"bad sequence":           func(trial *ForestAuditConfirmationTrial) { trial.Sequence = 6 },
		"bad digest":             func(trial *ForestAuditConfirmationTrial) { trial.RunConfigSHA256 = "unverified" },
		"unexpected predecessor": func(trial *ForestAuditConfirmationTrial) { trial.PreviousSHA256 = strings.Repeat("f", 64) },
		"bad repeats":            func(trial *ForestAuditConfirmationTrial) { trial.RepeatCount = 0 },
		"wrong mode":             func(trial *ForestAuditConfirmationTrial) { trial.Result.Mode = "c_oracle" },
		"failed result":          func(trial *ForestAuditConfirmationTrial) { trial.Result.Status = "fail" },
		"zero timing":            func(trial *ForestAuditConfirmationTrial) { trial.Result.RoutedNanos = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := trial
			mutate(&candidate)
			if err := validateForestAuditConfirmationTrial(candidate); err == nil {
				t.Fatalf("accepted invalid trial %#v", candidate)
			}
		})
	}
}

func TestForestAuditConfirmationRejectsZeroRoutedCoverage(t *testing.T) {
	result := forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000)
	result.Files[0].Routed = result.Files[0].Peer
	result.Files[0].RoutedProvenance = forestAuditRouteProductionFallback
	result.FilesRoutedForest = 0
	result.FilesRoutedFallback = 1
	if _, err := NewForestAuditConfirmationTrial(
		forestAuditConfirmationPairA,
		strings.Repeat("d", 64),
		"",
		ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1},
		result,
	); err == nil || !strings.Contains(err.Error(), "zero routed forest coverage") {
		t.Fatalf("zero confirmation coverage error = %v", err)
	}
	blockers := forestPromotionBlockers(&result, &ForestAuditResult{Status: "pass"})
	if !reflect.DeepEqual(blockers, []string{"no_routed_forest_coverage"}) {
		t.Fatalf("zero screen coverage blockers = %v", blockers)
	}
}

func TestValidateForestAuditConfirmationEvidenceRejectsCoveredPathDrift(t *testing.T) {
	digest := strings.Repeat("d", 64)
	screen := forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000)
	screen.Files = append(screen.Files, screen.Files[0])
	screen.Files[1].Path = "b.go"
	screen.Files[1].SHA256 = strings.Repeat("e", 64)
	screen.Files[1].Routed = screen.Files[1].Peer
	screen.Files[1].RoutedProvenance = forestAuditRouteProductionFallback
	screen.FilesRoutedFallback = 1
	screen.FilesTotal, screen.FilesAccepted = 2, 2
	screen.PeerNanos, screen.RoutedNanos, screen.RoutedImproved = 2_400_000_000, 2_000_000_000, true

	trialResult := screen
	trialResult.Files = append([]ForestAuditFileResult(nil), screen.Files...)
	trialResult.Files[0].Routed = trialResult.Files[0].Peer
	trialResult.Files[0].RoutedProvenance = forestAuditRouteProductionFallback
	trialResult.Files[1].Routed = forestAuditTestAcceptedOutcome(1, true)
	trialResult.Files[1].RoutedProvenance = forestAuditRouteForestFastPath
	trial, err := NewForestAuditConfirmationTrial(
		forestAuditConfirmationPairA,
		digest,
		"",
		ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1},
		trialResult,
	)
	if err != nil {
		t.Fatal(err)
	}
	cOracle := screen
	cOracle.Mode = "c_oracle"
	cOracle.Files = append([]ForestAuditFileResult(nil), screen.Files...)
	for i := range cOracle.Files {
		cOracle.Files[i].Peer = forestAuditTestAcceptedOutcome(uint32(cOracle.Files[i].Bytes), false)
	}
	reduction := &forestAuditReduction{
		results: map[string]map[string]*ForestAuditResult{
			"go": {"production": &screen, "c_oracle": &cOracle},
		},
		confirmations: map[string]map[string]ForestAuditConfirmationTrial{
			"go": {forestAuditConfirmationPairA: trial},
		},
		runConfigs: map[string]ForestAuditRunConfig{digest: {Schema: ForestAuditRunConfigSchema}},
	}
	if err := reduction.validateConfirmationEvidence(); err == nil || !strings.Contains(err.Error(), "differ from screen") {
		t.Fatalf("covered path drift error = %v", err)
	}
}

func TestValidateForestAuditConfirmationEvidenceRequiresPathCompleteCOracle(t *testing.T) {
	digest := strings.Repeat("d", 64)
	screen := forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000)
	trial, err := NewForestAuditConfirmationTrial(
		forestAuditConfirmationPairA, digest, "",
		ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1}, screen,
	)
	if err != nil {
		t.Fatal(err)
	}
	cOracle := screen
	cOracle.Mode = "c_oracle"
	cOracle.Files = append([]ForestAuditFileResult(nil), screen.Files...)
	cOracle.Files[0].Disposition = "declined"
	cOracle.Files[0].Decline = "not_covered"
	cOracle.Files[0].Peer = forestAuditTestNotRunOutcome(1)
	reduction := &forestAuditReduction{
		results:       map[string]map[string]*ForestAuditResult{"go": {"production": &screen, "c_oracle": &cOracle}},
		confirmations: map[string]map[string]ForestAuditConfirmationTrial{"go": {forestAuditConfirmationPairA: trial}},
		runConfigs:    map[string]ForestAuditRunConfig{digest: {Schema: ForestAuditRunConfigSchema}},
	}
	if err := reduction.validateConfirmationEvidence(); err == nil || !strings.Contains(err.Error(), "lacks accepted locked-C coverage") {
		t.Fatalf("path-incomplete C oracle error = %v", err)
	}
}

func TestReadForestAuditArtifactRejectsSymlinksAndOversizeFiles(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "artifact.json")
	if err := os.WriteFile(regular, []byte(`{"schema":"forest-audit-confirmation-plan-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := readForestAuditArtifact(symlink); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink artifact error = %v", err)
	}
	oversize := filepath.Join(dir, "oversize.json")
	file, err := os.Create(oversize)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(forestAuditMaxArtifactBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readForestAuditArtifact(oversize); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("oversize artifact error = %v", err)
	}
}

func TestCopyForestAuditArtifactExclusiveNeverReplacesOrExposesPartialTarget(t *testing.T) {
	dir := t.TempDir()
	data := []byte("{\"schema\":\"forest-audit-confirmation-plan-v1\"}\n")
	target := filepath.Join(dir, "receipt.json")
	if err := copyForestAuditArtifactExclusive(target, data); err != nil {
		t.Fatal(err)
	}
	if err := copyForestAuditArtifactExclusive(target, data); err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	partial := filepath.Join(dir, "partial.json")
	if err := os.WriteFile(partial, []byte("partial"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := copyForestAuditArtifactExclusive(partial, data); err == nil || !strings.Contains(err.Error(), "quarantine") {
		t.Fatalf("partial target error = %v", err)
	}
	got, err := os.ReadFile(partial)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "partial" {
		t.Fatalf("partial target was replaced: %q", got)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".forest-audit-publish-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("publisher temp leftovers = %v, err=%v", leftovers, err)
	}
}

func TestPublishForestAuditConfirmationIndexRejectsMismatchedCohortBeforeInstall(t *testing.T) {
	revision := strings.Repeat("a", 40)
	manifestSHA := strings.Repeat("b", 64)
	lockSHA := strings.Repeat("c", 64)
	base := ForestAuditConfirmationCohort{
		Schema: ForestAuditConfirmationCohortSchema, GotreesitterRevision: revision,
		CorpusManifestSHA256: manifestSHA, CorpusLockSHA256: lockSHA, Language: "go",
		RunConfigSHA256: strings.Repeat("d", 64), HeadTrialSHA256: strings.Repeat("e", 64),
		Trials: []ForestAuditConfirmationTrialRef{{TrialID: forestAuditConfirmationPairA, SHA256: strings.Repeat("e", 64)}},
	}
	for name, mutate := range map[string]func(*ForestAuditConfirmationCohort){
		"language": func(cohort *ForestAuditConfirmationCohort) { cohort.Language = "python" },
		"revision": func(cohort *ForestAuditConfirmationCohort) { cohort.GotreesitterRevision = strings.Repeat("f", 40) },
		"manifest": func(cohort *ForestAuditConfirmationCohort) { cohort.CorpusManifestSHA256 = strings.Repeat("f", 64) },
		"lock":     func(cohort *ForestAuditConfirmationCohort) { cohort.CorpusLockSHA256 = strings.Repeat("f", 64) },
		"shape": func(cohort *ForestAuditConfirmationCohort) {
			cohort.Trials[0].TrialID = forestAuditConfirmationPairB
		},
	} {
		t.Run(name, func(t *testing.T) {
			resultsRoot := t.TempDir()
			cohort := base
			cohort.Trials = append([]ForestAuditConfirmationTrialRef(nil), base.Trials...)
			mutate(&cohort)
			data, digest, err := marshalForestAuditContent(cohort)
			if err != nil {
				t.Fatal(err)
			}
			cohortPath := filepath.Join(resultsRoot, "confirmation", "cohorts", digest+".json")
			if err := copyForestAuditArtifactExclusive(cohortPath, data); err != nil {
				t.Fatal(err)
			}
			index := ForestAuditConfirmationIndex{
				Schema: ForestAuditConfirmationIndexSchema, GotreesitterRevision: revision,
				CorpusManifestSHA256: manifestSHA, CorpusLockSHA256: lockSHA,
				Cohorts: []ForestAuditConfirmationIndexEntry{{Language: "go", CohortSHA256: digest}},
			}
			if _, _, err := PublishForestAuditConfirmationIndex(resultsRoot, index); err == nil {
				t.Fatalf("published index with mismatched cohort %#v", cohort)
			}
			installed, err := filepath.Glob(filepath.Join(resultsRoot, "confirmation", "indexes", "*.json"))
			if err != nil || len(installed) != 0 {
				t.Fatalf("installed mismatched index artifacts = %v, err=%v", installed, err)
			}
		})
	}
}

func TestStoreForestAuditConfirmationValidatesPredecessorChainBeforePublication(t *testing.T) {
	for name, setupPredecessor := range map[string]func(*testing.T, string, string) string{
		"missing": func(_ *testing.T, _, _ string) string {
			return strings.Repeat("9", 64)
		},
		"corrupt": func(t *testing.T, resultsRoot, _ string) string {
			t.Helper()
			digest := strings.Repeat("9", 64)
			path := filepath.Join(resultsRoot, "confirmation", "trials", digest+".json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not json\n"), 0o444); err != nil {
				t.Fatal(err)
			}
			return digest
		},
		"identity mismatch": func(t *testing.T, resultsRoot, runConfigSHA string) string {
			t.Helper()
			result := forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000)
			result.Language = "python"
			predecessor, err := NewForestAuditConfirmationTrial(
				forestAuditConfirmationPairA,
				runConfigSHA,
				"",
				ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1},
				result,
			)
			if err != nil {
				t.Fatal(err)
			}
			data, digest, err := marshalForestAuditContent(predecessor)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(resultsRoot, "confirmation", "trials", digest+".json")
			if err := copyForestAuditArtifactExclusive(path, data); err != nil {
				t.Fatal(err)
			}
			return digest
		},
	} {
		t.Run(name, func(t *testing.T) {
			resultsRoot := t.TempDir()
			runConfigSHA, runConfigPath, _ := writeForestAuditTestRunConfig(t)
			previousSHA := setupPredecessor(t, resultsRoot, runConfigSHA)
			before := snapshotForestAuditConfirmationArtifacts(t, resultsRoot)

			trial, err := NewForestAuditConfirmationTrial(
				forestAuditConfirmationPairB,
				runConfigSHA,
				previousSHA,
				ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderRoutedFirst, RepeatCount: 1},
				forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000),
			)
			if err != nil {
				t.Fatal(err)
			}
			trialPath := filepath.Join(t.TempDir(), "pair-b.json")
			if err := WriteForestAuditConfirmationTrial(trialPath, trial); err != nil {
				t.Fatal(err)
			}
			if _, err := StoreForestAuditConfirmation(resultsRoot, runConfigPath, trialPath); err == nil {
				t.Fatalf("stored pair-b with %s predecessor", name)
			}
			after := snapshotForestAuditConfirmationArtifacts(t, resultsRoot)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("immutable artifacts changed after %s predecessor: before=%v after=%v", name, before, after)
			}
		})
	}
}

func snapshotForestAuditConfirmationArtifacts(t *testing.T, resultsRoot string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	root := filepath.Join(resultsRoot, "confirmation")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return snapshot
	} else if err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			snapshot[relative] = string(data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestSummarizeForestAuditConfirmationsRequiresOrderBalancedEvidence(t *testing.T) {
	digest := strings.Repeat("e", 64)
	pairA := forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairA, digest, 1, 1_200_000_000, 1_000_000_000)
	pairB := forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, digest, 1, 1_180_000_000, 1_000_000_000)

	tests := []struct {
		name       string
		trials     map[string]ForestAuditConfirmationTrial
		wantStatus string
		wantRepeat int
	}{
		{name: "no trials", trials: nil, wantStatus: forestAuditConfirmationRequired},
		{name: "forward only", trials: map[string]ForestAuditConfirmationTrial{forestAuditConfirmationPairA: pairA}, wantStatus: forestAuditConfirmationReverse},
		{name: "balanced stable win", trials: map[string]ForestAuditConfirmationTrial{
			forestAuditConfirmationPairA: pairA, forestAuditConfirmationPairB: pairB,
		}, wantStatus: forestAuditConfirmationConfirmed},
		{name: "short win escalates", trials: map[string]ForestAuditConfirmationTrial{
			forestAuditConfirmationPairA: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairA, digest, 1, 200_000_000, 100_000_000),
			forestAuditConfirmationPairB: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, digest, 1, 190_000_000, 100_000_000),
		}, wantStatus: forestAuditConfirmationABBA, wantRepeat: 8},
		{name: "marginal win escalates", trials: map[string]ForestAuditConfirmationTrial{
			forestAuditConfirmationPairA: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairA, digest, 1, 1_030_000_000, 1_000_000_000),
			forestAuditConfirmationPairB: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, digest, 1, 1_020_000_000, 1_000_000_000),
		}, wantStatus: forestAuditConfirmationABBA, wantRepeat: 1},
		{name: "order sign change escalates", trials: map[string]ForestAuditConfirmationTrial{
			forestAuditConfirmationPairA: pairA,
			forestAuditConfirmationPairB: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, digest, 1, 900_000_000, 1_000_000_000),
		}, wantStatus: forestAuditConfirmationABBA, wantRepeat: 1},
		{name: "stable clear loss stops", trials: map[string]ForestAuditConfirmationTrial{
			forestAuditConfirmationPairA: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairA, digest, 1, 900_000_000, 1_000_000_000),
			forestAuditConfirmationPairB: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, digest, 1, 950_000_000, 1_000_000_000),
		}, wantStatus: forestAuditConfirmationNeutral},
		{name: "config mismatch is inconclusive", trials: map[string]ForestAuditConfirmationTrial{
			forestAuditConfirmationPairA: pairA,
			forestAuditConfirmationPairB: forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, strings.Repeat("f", 64), 1, 1_180_000_000, 1_000_000_000),
		}, wantStatus: forestAuditConfirmationInconclusive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary := summarizeForestAuditConfirmations(tc.trials)
			if summary.Status != tc.wantStatus || summary.RequiredRepeatCount != tc.wantRepeat {
				t.Fatalf("summary = %#v", summary)
			}
			if len(tc.trials) > 0 && len(summary.CompletedTrialIDs) != len(tc.trials) {
				t.Fatalf("completed ids = %v, trials = %d", summary.CompletedTrialIDs, len(tc.trials))
			}
		})
	}
}

func TestSummarizeForestAuditConfirmationsAcceptsCompleteABBA(t *testing.T) {
	digest := strings.Repeat("a", 64)
	trials := map[string]ForestAuditConfirmationTrial{
		forestAuditConfirmationPairA:  forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairA, digest, 1, 1_030_000_000, 1_000_000_000),
		forestAuditConfirmationPairB:  forestAuditConfirmationTestTrial(t, forestAuditConfirmationPairB, digest, 1, 1_020_000_000, 1_000_000_000),
		forestAuditConfirmationABBAA1: forestAuditConfirmationTestTrial(t, forestAuditConfirmationABBAA1, digest, 2, 2_400_000_000, 2_000_000_000),
		forestAuditConfirmationABBAB1: forestAuditConfirmationTestTrial(t, forestAuditConfirmationABBAB1, digest, 2, 2_300_000_000, 2_000_000_000),
		forestAuditConfirmationABBAB2: forestAuditConfirmationTestTrial(t, forestAuditConfirmationABBAB2, digest, 2, 2_200_000_000, 2_000_000_000),
		forestAuditConfirmationABBAA2: forestAuditConfirmationTestTrial(t, forestAuditConfirmationABBAA2, digest, 2, 2_300_000_000, 2_000_000_000),
	}
	summary := summarizeForestAuditConfirmations(trials)
	if summary.Status != forestAuditConfirmationConfirmed || summary.TrialsComplete != len(trials) || len(summary.OrderSpeedups) != 4 {
		t.Fatalf("summary = %#v", summary)
	}
	mismatched := make(map[string]ForestAuditConfirmationTrial, len(trials))
	for id, trial := range trials {
		mismatched[id] = trial
	}
	trial := mismatched[forestAuditConfirmationABBAA2]
	trial.RunConfigSHA256 = strings.Repeat("b", 64)
	mismatched[forestAuditConfirmationABBAA2] = trial
	if summary := summarizeForestAuditConfirmations(mismatched); summary.Status != forestAuditConfirmationInconclusive {
		t.Fatalf("mismatched ABBA summary = %#v", summary)
	}
	highSpread := make(map[string]ForestAuditConfirmationTrial, len(trials))
	for id, trial := range trials {
		highSpread[id] = trial
	}
	trial = highSpread[forestAuditConfirmationABBAA1]
	trial.Result.PeerNanos = 3_000_000_000
	highSpread[forestAuditConfirmationABBAA1] = trial
	trial = highSpread[forestAuditConfirmationABBAB1]
	trial.Result.PeerNanos = 2_020_000_000
	highSpread[forestAuditConfirmationABBAB1] = trial
	if summary := summarizeForestAuditConfirmations(highSpread); summary.Status != forestAuditConfirmationInconclusive {
		t.Fatalf("high-spread ABBA summary = %#v", summary)
	}
}

func TestBuildForestAuditConfirmationPlanIsWinOnlyAndKeepsABBAOrder(t *testing.T) {
	report := ForestAuditReport{
		Schema: ForestAuditReportSchema, GotreesitterRevision: strings.Repeat("a", 40),
		CorpusManifestSHA256: strings.Repeat("b", 64), CorpusLockSHA256: strings.Repeat("c", 64),
		Languages: []ForestAuditLanguageReport{
			{Language: "skip-correction", ScreeningEligible: true, CorrectnessRelation: forestAuditRelationOracleCorrectionReview, ConfirmationStatus: forestAuditConfirmationRequired},
			{Language: "skip-inconclusive", ScreeningEligible: true, CorrectnessRelation: forestAuditRelationProductionIdentity, ConfirmationStatus: forestAuditConfirmationInconclusive},
			{Language: "skip-loss", CorrectnessRelation: forestAuditRelationProductionIdentity},
			{Language: "abba", ScreeningEligible: true, CorrectnessRelation: forestAuditRelationProductionIdentity, ConfirmationStatus: forestAuditConfirmationABBA,
				Confirmation: &ForestAuditConfirmationSummary{RequiredRepeatCount: 6, CompletedTrialIDs: []string{forestAuditConfirmationABBAA1}}},
			{Language: "fresh", ScreeningEligible: true, CorrectnessRelation: forestAuditRelationProductionIdentity, ConfirmationStatus: forestAuditConfirmationRequired},
		},
	}
	plan := BuildForestAuditConfirmationPlan(report)
	if err := validateForestAuditConfirmationPlan(plan); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(plan.Trials))
	for _, trial := range plan.Trials {
		got = append(got, trial.Language+":"+trial.TrialID+":"+trial.ExecutionOrder)
		if trial.Language == "abba" && trial.RepeatCount != 6 {
			t.Fatalf("ABBA repeat count = %d", trial.RepeatCount)
		}
	}
	want := []string{
		"abba:abba-b1:routed_first",
		"abba:abba-b2:routed_first",
		"abba:abba-a2:production_first",
		"fresh:pair-a:production_first",
		"fresh:pair-b:routed_first",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan order = %v, want %v", got, want)
	}
	invalid := plan
	invalid.Trials = append([]ForestAuditPlannedTrial(nil), plan.Trials...)
	invalid.Trials[0].ExecutionOrder = ForestAuditOrderProductionFirst
	if err := validateForestAuditConfirmationPlan(invalid); err == nil {
		t.Fatal("accepted plan with mismatched trial order")
	}
}

func TestForestAuditCorrectnessRelationLeavesOracleCorrectionForReview(t *testing.T) {
	production := forestAuditConfirmationTestResult(1_200_000_000, 1_000_000_000)
	production.Status = "fail"
	production.FilesDiverged = 1
	production.FilesRoutedDiverged = 1
	production.Files[0].Disposition = "diverged"
	production.Files[0].Diff = "forest matches C, not production"
	production.Files[0].RoutedDiff = "routed matches C, not production"

	cOracle := ForestAuditResult{
		Schema: ForestAuditResultSchema, Mode: "c_oracle", GotreesitterRevision: strings.Repeat("a", 40),
		CorpusManifestSHA256: strings.Repeat("b", 64), CorpusLockSHA256: strings.Repeat("c", 64),
		Language: "go", Status: "pass", FilesTotal: 1, FilesAccepted: 1,
		Files: []ForestAuditFileResult{{
			Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64), Disposition: "accepted",
			Forest: forestAuditTestAcceptedOutcome(1, true), Peer: forestAuditTestAcceptedOutcome(1, false),
			Routed: forestAuditTestNotRunOutcome(1), RoutedProvenance: forestAuditRouteNotRun,
		}},
	}
	relation, evidence := forestAuditCorrectnessRelation(&production, &cOracle)
	if relation != forestAuditRelationOracleCorrectionReview || evidence == nil || evidence.EvidenceComplete ||
		!reflect.DeepEqual(evidence.Paths, []string{"a.go"}) {
		t.Fatalf("relation=%q evidence=%#v", relation, evidence)
	}

	production.Files[0].RoutedProvenance = forestAuditRouteProductionFallback
	relation, evidence = forestAuditCorrectnessRelation(&production, &cOracle)
	if relation != forestAuditRelationUnresolvedDivergence || evidence == nil || evidence.EvidenceComplete {
		t.Fatalf("fallback relation=%q evidence=%#v", relation, evidence)
	}
}

func forestAuditConfirmationTestTrial(t *testing.T, id, digest string, repeats int, peerNanos, routedNanos int64) ForestAuditConfirmationTrial {
	t.Helper()
	order := ForestAuditOrderProductionFirst
	previousSHA := strings.Repeat("9", 64)
	if id == forestAuditConfirmationPairB || id == forestAuditConfirmationABBAB1 || id == forestAuditConfirmationABBAB2 {
		order = ForestAuditOrderRoutedFirst
	}
	if id == forestAuditConfirmationPairA {
		previousSHA = ""
	}
	trial, err := NewForestAuditConfirmationTrial(id, digest, previousSHA, ForestAuditRunPlan{ExecutionOrder: order, RepeatCount: repeats}, forestAuditConfirmationTestResult(peerNanos, routedNanos))
	if err != nil {
		t.Fatalf("build trial %s: %v", id, err)
	}
	return trial
}

func forestAuditConfirmationTestResult(peerNanos, routedNanos int64) ForestAuditResult {
	result := ForestAuditResult{
		Schema: ForestAuditResultSchema, Mode: "production", GotreesitterRevision: strings.Repeat("a", 40),
		CorpusManifestSHA256: strings.Repeat("b", 64), CorpusLockSHA256: strings.Repeat("c", 64),
		Language: "go", Status: "pass", FilesTotal: 1, FilesAccepted: 1, FilesRoutedForest: 1,
		PeerNanos: peerNanos, RoutedNanos: routedNanos, RoutedImproved: routedNanos < peerNanos,
	}
	result.Files = []ForestAuditFileResult{{
		Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64), Disposition: "accepted",
		Forest: forestAuditTestAcceptedOutcome(1, true), Peer: forestAuditTestAcceptedOutcome(1, false),
		Routed: forestAuditTestAcceptedOutcome(1, true), RoutedProvenance: forestAuditRouteForestFastPath, RoutedNanos: routedNanos,
	}}
	return result
}
