package cgoharness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	ForestAuditConfirmationTrialSchema  = "forest-audit-confirmation-trial-v1"
	ForestAuditConfirmationPlanSchema   = "forest-audit-confirmation-plan-v1"
	ForestAuditRunConfigSchema          = "forest-audit-run-config-v1"
	ForestAuditConfirmationCohortSchema = "forest-audit-confirmation-cohort-v1"
	ForestAuditConfirmationIndexSchema  = "forest-audit-confirmation-index-v1"

	ForestAuditOrderProductionFirst = "production_first"
	ForestAuditOrderRoutedFirst     = "routed_first"

	forestAuditConfirmationPairA  = "pair-a"
	forestAuditConfirmationPairB  = "pair-b"
	forestAuditConfirmationABBAA1 = "abba-a1"
	forestAuditConfirmationABBAB1 = "abba-b1"
	forestAuditConfirmationABBAB2 = "abba-b2"
	forestAuditConfirmationABBAA2 = "abba-a2"

	forestAuditConfirmationRequired     = "confirmation_required"
	forestAuditConfirmationReverse      = "reverse_required"
	forestAuditConfirmationABBA         = "abba_required"
	forestAuditConfirmationConfirmed    = "confirmed"
	forestAuditConfirmationNeutral      = "economically_neutral"
	forestAuditConfirmationInconclusive = "inconclusive"

	forestAuditRelationProductionIdentity     = "production_identity"
	forestAuditRelationOracleCorrectionReview = "oracle_correction_review_required"
	forestAuditRelationUnresolvedDivergence   = "unresolved_divergence"
)

const (
	forestAuditConfirmationMinSpeedup = 1.05
	forestAuditConfirmationMaxSpread  = 1.10
	forestAuditConfirmationMinNanos   = int64(750_000_000)
	forestAuditConfirmationMaxRepeats = 20
)

// ForestAuditRunPlan controls pair order and the number of complete-corpus
// production/routed sweeps inside one isolated language shard. The default
// exactly preserves the v2 screening behavior.
type ForestAuditRunPlan struct {
	ExecutionOrder string
	RepeatCount    int
}

func DefaultForestAuditRunPlan() ForestAuditRunPlan {
	return ForestAuditRunPlan{ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1}
}

func ForestAuditRunPlanFromEnv() (ForestAuditRunPlan, error) {
	plan := DefaultForestAuditRunPlan()
	if raw := strings.TrimSpace(os.Getenv("GTS_FOREST_AUDIT_EXECUTION_ORDER")); raw != "" {
		plan.ExecutionOrder = raw
	}
	if raw := strings.TrimSpace(os.Getenv("GTS_FOREST_AUDIT_REPEAT_COUNT")); raw != "" {
		repeats, err := strconv.Atoi(raw)
		if err != nil {
			return plan, fmt.Errorf("parse GTS_FOREST_AUDIT_REPEAT_COUNT: %w", err)
		}
		plan.RepeatCount = repeats
	}
	return plan, validateForestAuditRunPlan(plan)
}

func validateForestAuditRunPlan(plan ForestAuditRunPlan) error {
	if plan.ExecutionOrder != ForestAuditOrderProductionFirst && plan.ExecutionOrder != ForestAuditOrderRoutedFirst {
		return fmt.Errorf("invalid forest audit execution order %q", plan.ExecutionOrder)
	}
	if plan.RepeatCount < 1 || plan.RepeatCount > forestAuditConfirmationMaxRepeats {
		return fmt.Errorf("forest audit repeat count %d outside 1..%d", plan.RepeatCount, forestAuditConfirmationMaxRepeats)
	}
	return nil
}

// ForestAuditConfirmationTrial is a self-describing timing block. Its nested
// result remains forest-audit-result-v2 so historical screening shards retain
// their exact schema and validation contract.
type ForestAuditConfirmationTrial struct {
	Schema          string            `json:"schema"`
	TrialID         string            `json:"trial_id"`
	Sequence        int               `json:"sequence"`
	ExecutionOrder  string            `json:"execution_order"`
	RepeatCount     int               `json:"repeat_count"`
	RunConfigSHA256 string            `json:"run_config_sha256"`
	PreviousSHA256  string            `json:"previous_trial_sha256,omitempty"`
	Result          ForestAuditResult `json:"result"`
	receiptSHA256   string
	receiptPath     string
}

type ForestAuditRunConfig struct {
	Schema          string `json:"schema"`
	HostLabel       string `json:"host_label"`
	HostFingerprint string `json:"host_fingerprint"`
	ImageID         string `json:"image_id"`
	Memory          string `json:"memory"`
	CPUs            string `json:"cpus"`
	CPUSetCPUs      string `json:"cpuset_cpus"`
	PIDs            string `json:"pids"`
	GoMemLimit      string `json:"gomemlimit"`
	GOMAXPROCS      int    `json:"gomaxprocs"`
	TestTimeout     string `json:"timeout"`
}

type ForestAuditConfirmationTrialRef struct {
	TrialID string `json:"trial_id"`
	SHA256  string `json:"sha256"`
}

type ForestAuditConfirmationCohort struct {
	Schema               string                            `json:"schema"`
	GotreesitterRevision string                            `json:"gotreesitter_revision"`
	CorpusManifestSHA256 string                            `json:"corpus_manifest_sha256"`
	CorpusLockSHA256     string                            `json:"corpus_lock_sha256"`
	Language             string                            `json:"language"`
	RunConfigSHA256      string                            `json:"run_config_sha256"`
	HeadTrialSHA256      string                            `json:"head_trial_sha256"`
	Trials               []ForestAuditConfirmationTrialRef `json:"trials"`
}

type ForestAuditConfirmationIndexEntry struct {
	Language     string `json:"language"`
	CohortSHA256 string `json:"cohort_sha256"`
}

type ForestAuditConfirmationIndex struct {
	Schema               string                              `json:"schema"`
	GotreesitterRevision string                              `json:"gotreesitter_revision"`
	CorpusManifestSHA256 string                              `json:"corpus_manifest_sha256"`
	CorpusLockSHA256     string                              `json:"corpus_lock_sha256"`
	Cohorts              []ForestAuditConfirmationIndexEntry `json:"cohorts"`
}

type ForestAuditStoredConfirmation struct {
	TrialSHA256  string
	CohortSHA256 string
	IndexSHA256  string
	IndexPath    string
}

func PublishForestAuditConfirmationIndex(resultsRoot string, index ForestAuditConfirmationIndex) (string, string, error) {
	if index.Schema != ForestAuditConfirmationIndexSchema {
		return "", "", fmt.Errorf("confirmation index schema %q", index.Schema)
	}
	if err := forestManifestRevision(index.GotreesitterRevision); err != nil {
		return "", "", err
	}
	if _, err := forestManifestDigest("manifest", index.CorpusManifestSHA256); err != nil {
		return "", "", err
	}
	if _, err := forestManifestDigest("corpus lock", index.CorpusLockSHA256); err != nil {
		return "", "", err
	}
	sort.Slice(index.Cohorts, func(i, j int) bool { return index.Cohorts[i].Language < index.Cohorts[j].Language })
	previous := ""
	for _, entry := range index.Cohorts {
		if err := forestManifestLanguage(entry.Language); err != nil {
			return "", "", err
		}
		if entry.Language <= previous {
			return "", "", fmt.Errorf("confirmation index cohorts are duplicate")
		}
		if _, err := forestManifestDigest("cohort", entry.CohortSHA256); err != nil {
			return "", "", err
		}
		cohortPath := filepath.Join(resultsRoot, "confirmation", "cohorts", entry.CohortSHA256+".json")
		artifact, err := readForestAuditArtifact(cohortPath)
		if err != nil || artifact.schema != ForestAuditConfirmationCohortSchema || artifact.digest != entry.CohortSHA256 {
			return "", "", fmt.Errorf("selected cohort for %s is unavailable or invalid", entry.Language)
		}
		var cohort ForestAuditConfirmationCohort
		if err := artifact.decode(&cohort); err != nil {
			return "", "", fmt.Errorf("decode selected cohort for %s: %w", entry.Language, err)
		}
		if err := validateForestAuditConfirmationCohortIdentity(
			cohort, entry.Language, index.GotreesitterRevision, index.CorpusManifestSHA256, index.CorpusLockSHA256,
		); err != nil {
			return "", "", err
		}
		previous = entry.Language
	}
	if len(index.Cohorts) == 0 {
		return "", "", fmt.Errorf("confirmation index has no cohorts")
	}
	data, digest, err := marshalForestAuditContent(index)
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(resultsRoot, "confirmation", "indexes", digest+".json")
	if err := copyForestAuditArtifactExclusive(path, data); err != nil {
		return "", "", err
	}
	return digest, path, nil
}

func NewForestAuditConfirmationTrial(trialID, runConfigSHA, previousTrialSHA string, plan ForestAuditRunPlan, result ForestAuditResult) (ForestAuditConfirmationTrial, error) {
	spec, ok := forestAuditConfirmationTrialSpec(trialID)
	if !ok {
		return ForestAuditConfirmationTrial{}, fmt.Errorf("invalid forest confirmation trial id %q", trialID)
	}
	trial := ForestAuditConfirmationTrial{
		Schema: ForestAuditConfirmationTrialSchema, TrialID: trialID, Sequence: spec.sequence,
		ExecutionOrder: plan.ExecutionOrder, RepeatCount: plan.RepeatCount,
		RunConfigSHA256: strings.ToLower(strings.TrimSpace(runConfigSHA)),
		PreviousSHA256:  strings.ToLower(strings.TrimSpace(previousTrialSHA)), Result: result,
	}
	return trial, validateForestAuditConfirmationTrial(trial)
}

func WriteForestAuditConfirmationTrial(path string, trial ForestAuditConfirmationTrial) error {
	if err := validateForestAuditConfirmationTrial(trial); err != nil {
		return err
	}
	return writeForestAuditJSONExclusive(path, trial)
}

func (reduction *forestAuditReduction) admitConfirmationTrial(artifact forestAuditArtifact, expectedSHA string) error {
	var trial ForestAuditConfirmationTrial
	if err := artifact.decode(&trial); err != nil {
		return fmt.Errorf("read confirmation trial %s: %w", artifact.path, err)
	}
	if err := validateForestAuditConfirmationTrial(trial); err != nil {
		return fmt.Errorf("validate confirmation trial %s: %w", artifact.path, err)
	}
	if err := reduction.validateResultIdentity(trial.Result); err != nil {
		return fmt.Errorf("confirmation trial %s: %w", artifact.path, err)
	}
	if artifact.digest != expectedSHA || filepath.Base(artifact.path) != expectedSHA+".json" {
		return fmt.Errorf("confirmation trial %s content hash is %s, want %s", artifact.path, artifact.digest, expectedSHA)
	}
	trial.receiptSHA256 = artifact.digest
	trial.receiptPath = artifact.path
	language := trial.Result.Language
	if reduction.confirmations[language] == nil {
		reduction.confirmations[language] = make(map[string]ForestAuditConfirmationTrial)
	}
	if _, exists := reduction.confirmations[language][trial.TrialID]; exists {
		return fmt.Errorf("duplicate confirmation trial %q for %s", trial.TrialID, language)
	}
	reduction.confirmations[language][trial.TrialID] = trial
	return nil
}

func (reduction *forestAuditReduction) admitRunConfig(artifact forestAuditArtifact, expectedSHA string) error {
	var config ForestAuditRunConfig
	if err := artifact.decode(&config); err != nil {
		return fmt.Errorf("read forest audit run config %s: %w", artifact.path, err)
	}
	if err := validateForestAuditRunConfig(config); err != nil {
		return fmt.Errorf("validate forest audit run config %s: %w", artifact.path, err)
	}
	if artifact.digest != expectedSHA || filepath.Base(artifact.path) != expectedSHA+".json" {
		return fmt.Errorf("run-config receipt %s content hash is %s, want %s", artifact.path, artifact.digest, expectedSHA)
	}
	if _, exists := reduction.runConfigs[expectedSHA]; exists {
		return nil
	}
	reduction.runConfigs[expectedSHA] = config
	return nil
}

func (reduction *forestAuditReduction) loadConfirmationIndex(resultsRoot, indexPath string) error {
	indexArtifact, err := readForestAuditArtifact(indexPath)
	if err != nil {
		return fmt.Errorf("read confirmation index: %w", err)
	}
	if indexArtifact.schema != ForestAuditConfirmationIndexSchema {
		return fmt.Errorf("confirmation index schema %q, want %q", indexArtifact.schema, ForestAuditConfirmationIndexSchema)
	}
	if filepath.Base(indexPath) != indexArtifact.digest+".json" {
		return fmt.Errorf("confirmation index %s is not named by content hash %s", indexPath, indexArtifact.digest)
	}
	var index ForestAuditConfirmationIndex
	if err := indexArtifact.decode(&index); err != nil {
		return fmt.Errorf("decode confirmation index: %w", err)
	}
	if err := reduction.validateConfirmationIndex(index); err != nil {
		return err
	}
	reduction.confirmationIndexDigest = indexArtifact.digest
	for _, entry := range index.Cohorts {
		cohortPath := filepath.Join(resultsRoot, "confirmation", "cohorts", entry.CohortSHA256+".json")
		cohortArtifact, err := readForestAuditArtifact(cohortPath)
		if err != nil {
			return fmt.Errorf("read confirmation cohort for %s: %w", entry.Language, err)
		}
		if cohortArtifact.schema != ForestAuditConfirmationCohortSchema || cohortArtifact.digest != entry.CohortSHA256 {
			return fmt.Errorf("confirmation cohort for %s does not match selected digest %s", entry.Language, entry.CohortSHA256)
		}
		var cohort ForestAuditConfirmationCohort
		if err := cohortArtifact.decode(&cohort); err != nil {
			return fmt.Errorf("decode confirmation cohort for %s: %w", entry.Language, err)
		}
		if err := reduction.validateConfirmationCohort(cohort, entry.Language); err != nil {
			return err
		}
		reduction.confirmationCohorts[entry.Language] = cohortArtifact.digest
		configPath := filepath.Join(resultsRoot, "confirmation", "run-configs", cohort.RunConfigSHA256+".json")
		configArtifact, err := readForestAuditArtifact(configPath)
		if err != nil {
			return fmt.Errorf("read run config for %s: %w", entry.Language, err)
		}
		if err := reduction.admitRunConfig(configArtifact, cohort.RunConfigSHA256); err != nil {
			return err
		}
		for _, ref := range cohort.Trials {
			trialPath := filepath.Join(resultsRoot, "confirmation", "trials", ref.SHA256+".json")
			trialArtifact, err := readForestAuditArtifact(trialPath)
			if err != nil {
				return fmt.Errorf("read confirmation trial %s/%s: %w", entry.Language, ref.TrialID, err)
			}
			if trialArtifact.schema != ForestAuditConfirmationTrialSchema {
				return fmt.Errorf("confirmation trial %s/%s schema %q", entry.Language, ref.TrialID, trialArtifact.schema)
			}
			if err := reduction.admitConfirmationTrial(trialArtifact, ref.SHA256); err != nil {
				return err
			}
			admitted := reduction.confirmations[entry.Language][ref.TrialID]
			if admitted.TrialID != ref.TrialID || admitted.RunConfigSHA256 != cohort.RunConfigSHA256 {
				return fmt.Errorf("confirmation cohort reference %s/%s does not match decoded trial", entry.Language, ref.TrialID)
			}
		}
	}
	return nil
}

func (reduction *forestAuditReduction) validateConfirmationIndex(index ForestAuditConfirmationIndex) error {
	if index.Schema != ForestAuditConfirmationIndexSchema ||
		index.GotreesitterRevision != reduction.manifest.GotreesitterRevision ||
		index.CorpusManifestSHA256 != reduction.manifestDigest ||
		index.CorpusLockSHA256 != reduction.manifest.CorpusLock.SHA256 {
		return fmt.Errorf("confirmation index identity does not match manifest")
	}
	previous := ""
	for i, entry := range index.Cohorts {
		if err := forestManifestLanguage(entry.Language); err != nil {
			return fmt.Errorf("confirmation index cohort %d: %w", i, err)
		}
		if _, ok := reduction.manifestFiles[entry.Language]; !ok {
			return fmt.Errorf("confirmation index language %q absent from manifest", entry.Language)
		}
		if _, err := forestManifestDigest("cohort", entry.CohortSHA256); err != nil {
			return err
		}
		if entry.Language <= previous {
			return fmt.Errorf("confirmation index cohorts are duplicate or unsorted")
		}
		previous = entry.Language
	}
	if len(index.Cohorts) == 0 {
		return fmt.Errorf("confirmation index has no cohorts")
	}
	return nil
}

func (reduction *forestAuditReduction) validateConfirmationCohort(cohort ForestAuditConfirmationCohort, language string) error {
	return validateForestAuditConfirmationCohortIdentity(
		cohort, language, reduction.manifest.GotreesitterRevision, reduction.manifestDigest, reduction.manifest.CorpusLock.SHA256,
	)
}

func validateForestAuditConfirmationCohortIdentity(cohort ForestAuditConfirmationCohort, language, revision, manifestSHA, lockSHA string) error {
	if cohort.Schema != ForestAuditConfirmationCohortSchema || cohort.Language != language ||
		cohort.GotreesitterRevision != revision || cohort.CorpusManifestSHA256 != manifestSHA || cohort.CorpusLockSHA256 != lockSHA {
		return fmt.Errorf("confirmation cohort identity does not match manifest for %s", language)
	}
	if _, err := forestManifestDigest("run config", cohort.RunConfigSHA256); err != nil {
		return err
	}
	if len(cohort.Trials) == 0 || len(cohort.Trials) > 6 {
		return fmt.Errorf("confirmation cohort for %s has %d trials", language, len(cohort.Trials))
	}
	for i, ref := range cohort.Trials {
		spec, ok := forestAuditConfirmationTrialSpec(ref.TrialID)
		if !ok || spec.sequence != i+1 {
			return fmt.Errorf("confirmation cohort for %s has invalid trial %q at sequence %d", language, ref.TrialID, i+1)
		}
		if _, err := forestManifestDigest("trial", ref.SHA256); err != nil {
			return err
		}
	}
	if cohort.HeadTrialSHA256 != cohort.Trials[len(cohort.Trials)-1].SHA256 {
		return fmt.Errorf("confirmation cohort head for %s does not match final trial", language)
	}
	return nil
}

func StoreForestAuditConfirmation(resultsRoot, runConfigPath, trialPath string) (ForestAuditStoredConfirmation, error) {
	var stored ForestAuditStoredConfirmation
	runConfigArtifact, err := readForestAuditArtifact(runConfigPath)
	if err != nil {
		return stored, err
	}
	if runConfigArtifact.schema != ForestAuditRunConfigSchema {
		return stored, fmt.Errorf("run config schema %q", runConfigArtifact.schema)
	}
	var runConfig ForestAuditRunConfig
	if err := runConfigArtifact.decode(&runConfig); err != nil {
		return stored, err
	}
	if err := validateForestAuditRunConfig(runConfig); err != nil {
		return stored, err
	}
	trialArtifact, err := readForestAuditArtifact(trialPath)
	if err != nil {
		return stored, err
	}
	if trialArtifact.schema != ForestAuditConfirmationTrialSchema {
		return stored, fmt.Errorf("confirmation trial schema %q", trialArtifact.schema)
	}
	var trial ForestAuditConfirmationTrial
	if err := trialArtifact.decode(&trial); err != nil {
		return stored, err
	}
	if err := validateForestAuditConfirmationTrial(trial); err != nil {
		return stored, err
	}
	if trial.RunConfigSHA256 != runConfigArtifact.digest {
		return stored, fmt.Errorf("trial run config %s does not match receipt %s", trial.RunConfigSHA256, runConfigArtifact.digest)
	}
	trial.receiptSHA256 = trialArtifact.digest
	refs, err := forestAuditConfirmationRefs(resultsRoot, trial)
	if err != nil {
		return stored, err
	}
	trialPathInBundle := filepath.Join(resultsRoot, "confirmation", "trials", trialArtifact.digest+".json")
	configPathInBundle := filepath.Join(resultsRoot, "confirmation", "run-configs", runConfigArtifact.digest+".json")
	if err := copyForestAuditArtifactExclusive(configPathInBundle, runConfigArtifact.data); err != nil {
		return stored, err
	}
	if err := copyForestAuditArtifactExclusive(trialPathInBundle, trialArtifact.data); err != nil {
		return stored, err
	}
	cohort := ForestAuditConfirmationCohort{
		Schema: ForestAuditConfirmationCohortSchema, GotreesitterRevision: trial.Result.GotreesitterRevision,
		CorpusManifestSHA256: trial.Result.CorpusManifestSHA256, CorpusLockSHA256: trial.Result.CorpusLockSHA256,
		Language: trial.Result.Language, RunConfigSHA256: trial.RunConfigSHA256,
		HeadTrialSHA256: trialArtifact.digest, Trials: refs,
	}
	cohortData, cohortSHA, err := marshalForestAuditContent(cohort)
	if err != nil {
		return stored, err
	}
	cohortPath := filepath.Join(resultsRoot, "confirmation", "cohorts", cohortSHA+".json")
	if err := copyForestAuditArtifactExclusive(cohortPath, cohortData); err != nil {
		return stored, err
	}
	index := ForestAuditConfirmationIndex{
		Schema: ForestAuditConfirmationIndexSchema, GotreesitterRevision: trial.Result.GotreesitterRevision,
		CorpusManifestSHA256: trial.Result.CorpusManifestSHA256, CorpusLockSHA256: trial.Result.CorpusLockSHA256,
		Cohorts: []ForestAuditConfirmationIndexEntry{{Language: trial.Result.Language, CohortSHA256: cohortSHA}},
	}
	indexData, indexSHA, err := marshalForestAuditContent(index)
	if err != nil {
		return stored, err
	}
	indexPath := filepath.Join(resultsRoot, "confirmation", "indexes", indexSHA+".json")
	if err := copyForestAuditArtifactExclusive(indexPath, indexData); err != nil {
		return stored, err
	}
	return ForestAuditStoredConfirmation{TrialSHA256: trialArtifact.digest, CohortSHA256: cohortSHA, IndexSHA256: indexSHA, IndexPath: indexPath}, nil
}

func forestAuditConfirmationRefs(resultsRoot string, trial ForestAuditConfirmationTrial) ([]ForestAuditConfirmationTrialRef, error) {
	spec, _ := forestAuditConfirmationTrialSpec(trial.TrialID)
	refs := make([]ForestAuditConfirmationTrialRef, 0, spec.sequence)
	if spec.previousID != "" {
		previousPath := filepath.Join(resultsRoot, "confirmation", "trials", trial.PreviousSHA256+".json")
		previousArtifact, err := readForestAuditArtifact(previousPath)
		if err != nil {
			return nil, fmt.Errorf("read predecessor %s: %w", spec.previousID, err)
		}
		var previous ForestAuditConfirmationTrial
		if previousArtifact.schema != ForestAuditConfirmationTrialSchema || previousArtifact.digest != trial.PreviousSHA256 || previousArtifact.decode(&previous) != nil {
			return nil, fmt.Errorf("invalid predecessor receipt %s", trial.PreviousSHA256)
		}
		previous.receiptSHA256 = previousArtifact.digest
		if err := validateForestAuditConfirmationTrial(previous); err != nil {
			return nil, fmt.Errorf("invalid predecessor receipt %s: %w", trial.PreviousSHA256, err)
		}
		previousRefs, err := forestAuditConfirmationRefs(resultsRoot, previous)
		if err != nil {
			return nil, err
		}
		if previous.TrialID != spec.previousID || previous.Result.Language != trial.Result.Language ||
			previous.RunConfigSHA256 != trial.RunConfigSHA256 || previous.Result.GotreesitterRevision != trial.Result.GotreesitterRevision ||
			previous.Result.CorpusManifestSHA256 != trial.Result.CorpusManifestSHA256 || previous.Result.CorpusLockSHA256 != trial.Result.CorpusLockSHA256 {
			return nil, fmt.Errorf("predecessor identity does not match trial %s", trial.TrialID)
		}
		refs = append(refs, previousRefs...)
	}
	return append(refs, ForestAuditConfirmationTrialRef{TrialID: trial.TrialID, SHA256: trial.receiptSHA256}), nil
}

func marshalForestAuditContent(value any) ([]byte, string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, "", err
	}
	data = append(data, '\n')
	return data, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func copyForestAuditArtifactExclusive(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".forest-audit-publish-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o444); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempPath, path); err != nil {
		if os.IsExist(err) {
			existing, readErr := readForestAuditArtifact(path)
			if readErr == nil && fmt.Sprintf("%x", sha256.Sum256(data)) == existing.digest && string(existing.data) == string(data) {
				return syncForestAuditDirectory(dir)
			}
			if readErr != nil {
				return fmt.Errorf("publish immutable forest audit artifact %s: existing target is invalid; quarantine it before retrying: %w", path, readErr)
			}
			return fmt.Errorf("publish immutable forest audit artifact %s: existing target has different content; quarantine it before retrying", path)
		}
		return fmt.Errorf("publish immutable forest audit artifact %s: %w", path, err)
	}
	return syncForestAuditDirectory(dir)
}

func syncForestAuditDirectory(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func validateForestAuditRunConfig(config ForestAuditRunConfig) error {
	if config.Schema != ForestAuditRunConfigSchema {
		return fmt.Errorf("forest audit run config schema %q", config.Schema)
	}
	if strings.TrimSpace(config.HostLabel) == "" || strings.TrimSpace(config.HostFingerprint) == "" || strings.TrimSpace(config.ImageID) == "" ||
		strings.TrimSpace(config.Memory) == "" || strings.TrimSpace(config.CPUs) == "" ||
		strings.TrimSpace(config.CPUSetCPUs) == "" || strings.TrimSpace(config.PIDs) == "" ||
		strings.TrimSpace(config.GoMemLimit) == "" || strings.TrimSpace(config.TestTimeout) == "" {
		return fmt.Errorf("forest audit run config lacks an isolation identity")
	}
	if !strings.HasPrefix(config.ImageID, "sha256:") {
		return fmt.Errorf("forest audit image id %q is not content-addressed", config.ImageID)
	}
	if _, err := forestManifestDigest("image", strings.TrimPrefix(config.ImageID, "sha256:")); err != nil {
		return err
	}
	if _, err := forestManifestDigest("host fingerprint", config.HostFingerprint); err != nil {
		return err
	}
	if config.CPUs != "1" {
		return fmt.Errorf("forest audit CPU limit %q, want 1", config.CPUs)
	}
	if _, err := strconv.Atoi(config.CPUSetCPUs); err != nil || strings.ContainsAny(config.CPUSetCPUs, ",- ") {
		return fmt.Errorf("forest audit CPU set %q is not one numeric CPU", config.CPUSetCPUs)
	}
	if config.GOMAXPROCS != 1 {
		return fmt.Errorf("forest audit GOMAXPROCS=%d, want 1", config.GOMAXPROCS)
	}
	return nil
}

func (reduction *forestAuditReduction) validateConfirmationEvidence() error {
	languages := make([]string, 0, len(reduction.confirmations))
	for language := range reduction.confirmations {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	for _, language := range languages {
		trials := reduction.confirmations[language]
		production := reduction.results[language]["production"]
		cOracle := reduction.results[language]["c_oracle"]
		if production == nil {
			return fmt.Errorf("confirmation evidence for %s lacks its production screen", language)
		}
		if cOracle == nil {
			return fmt.Errorf("confirmation evidence for %s lacks its locked C oracle", language)
		}
		screenPaths, err := forestAuditRoutedCoveragePaths(production)
		if err != nil {
			return fmt.Errorf("production screen %s: %w", language, err)
		}
		if err := forestAuditRequireCOracleCoverage(cOracle, screenPaths); err != nil {
			return fmt.Errorf("confirmation evidence for %s: %w", language, err)
		}
		trialIDs := make([]string, 0, len(trials))
		for trialID := range trials {
			trialIDs = append(trialIDs, trialID)
		}
		sort.Slice(trialIDs, func(i, j int) bool {
			left, _ := forestAuditConfirmationTrialSpec(trialIDs[i])
			right, _ := forestAuditConfirmationTrialSpec(trialIDs[j])
			return left.sequence < right.sequence
		})
		for _, trialID := range trialIDs {
			trial := trials[trialID]
			if _, ok := reduction.runConfigs[trial.RunConfigSHA256]; !ok {
				return fmt.Errorf("confirmation trial %s/%s is missing run-config receipt %s", language, trialID, trial.RunConfigSHA256)
			}
			trialPaths, err := forestAuditRoutedCoveragePaths(&trial.Result)
			if err != nil {
				return fmt.Errorf("confirmation trial %s/%s: %w", language, trialID, err)
			}
			if !forestAuditSamePaths(screenPaths, trialPaths) {
				return fmt.Errorf("confirmation trial %s/%s routed forest paths %v differ from screen %v", language, trialID, trialPaths, screenPaths)
			}
			spec, _ := forestAuditConfirmationTrialSpec(trialID)
			if spec.previousID == "" {
				continue
			}
			previous, ok := trials[spec.previousID]
			if !ok {
				return fmt.Errorf("confirmation trial %s/%s lacks predecessor %s", language, trialID, spec.previousID)
			}
			if previous.receiptSHA256 == "" || trial.PreviousSHA256 != previous.receiptSHA256 {
				return fmt.Errorf("confirmation trial %s/%s predecessor hash does not match %s", language, trialID, spec.previousID)
			}
		}
	}
	return nil
}

func forestAuditRequireCOracleCoverage(cOracle *ForestAuditResult, routedPaths []string) error {
	if cOracle == nil || cOracle.Mode != "c_oracle" || cOracle.Status != "pass" {
		return fmt.Errorf("locked C oracle is not a passing c_oracle result")
	}
	accepted := make(map[string]bool, len(cOracle.Files))
	for _, file := range cOracle.Files {
		if file.Disposition == "accepted" && file.Diff == "" && file.Peer.Accepted && file.Peer.FullSpan {
			accepted[file.Path] = true
		}
	}
	for _, path := range routedPaths {
		if !accepted[path] {
			return fmt.Errorf("routed forest path %q lacks accepted locked-C coverage", path)
		}
	}
	return nil
}

func forestAuditRoutedCoveragePaths(result *ForestAuditResult) ([]string, error) {
	if result == nil || result.FilesRoutedForest == 0 {
		return nil, fmt.Errorf("zero routed forest coverage")
	}
	paths := make([]string, 0, result.FilesRoutedForest)
	for _, file := range result.Files {
		if file.RoutedProvenance == forestAuditRouteForestFastPath {
			paths = append(paths, file.Path)
		}
	}
	if len(paths) != result.FilesRoutedForest {
		return nil, fmt.Errorf("routed forest coverage count %d does not match %d paths", result.FilesRoutedForest, len(paths))
	}
	sort.Strings(paths)
	return paths, nil
}

func forestAuditSamePaths(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func validateForestAuditConfirmationTrial(trial ForestAuditConfirmationTrial) error {
	if trial.Schema != ForestAuditConfirmationTrialSchema {
		return fmt.Errorf("forest confirmation trial schema %q", trial.Schema)
	}
	spec, ok := forestAuditConfirmationTrialSpec(trial.TrialID)
	if !ok {
		return fmt.Errorf("invalid forest confirmation trial id %q", trial.TrialID)
	}
	if err := validateForestAuditTrialID(trial.TrialID, trial.ExecutionOrder); err != nil {
		return err
	}
	if trial.Sequence != spec.sequence {
		return fmt.Errorf("forest confirmation trial %q sequence %d, want %d", trial.TrialID, trial.Sequence, spec.sequence)
	}
	if err := validateForestAuditRunPlan(ForestAuditRunPlan{ExecutionOrder: trial.ExecutionOrder, RepeatCount: trial.RepeatCount}); err != nil {
		return err
	}
	if _, err := forestManifestDigest("run config", trial.RunConfigSHA256); err != nil {
		return err
	}
	if err := validateForestAuditPreviousTrialSHA(trial.TrialID, trial.PreviousSHA256); err != nil {
		return err
	}
	if err := validateForestAuditResult(trial.Result); err != nil {
		return fmt.Errorf("confirmation result: %w", err)
	}
	if trial.Result.Mode != "production" {
		return fmt.Errorf("confirmation trial mode %q, want production", trial.Result.Mode)
	}
	if trial.Result.FilesRoutedForest == 0 {
		return fmt.Errorf("confirmation trial %q has zero routed forest coverage", trial.TrialID)
	}
	if trial.Result.Status != "pass" || trial.Result.FilesDiverged != 0 || trial.Result.FilesRoutedDiverged != 0 {
		return fmt.Errorf("confirmation trial %q is failed evidence and cannot be published", trial.TrialID)
	}
	if trial.Result.PeerNanos <= 0 || trial.Result.RoutedNanos <= 0 {
		return fmt.Errorf("confirmation trial %q lacks positive peer and routed timings", trial.TrialID)
	}
	return nil
}

func validateForestAuditPreviousTrialSHA(trialID, previousSHA string) error {
	spec, ok := forestAuditConfirmationTrialSpec(trialID)
	if !ok {
		return fmt.Errorf("invalid forest confirmation trial id %q", trialID)
	}
	if spec.previousID == "" {
		if previousSHA != "" {
			return fmt.Errorf("forest confirmation trial %q unexpectedly chains a predecessor", trialID)
		}
		return nil
	}
	_, err := forestManifestDigest("previous trial", previousSHA)
	return err
}

func validateForestAuditTrialID(trialID, order string) error {
	spec, ok := forestAuditConfirmationTrialSpec(trialID)
	if !ok {
		return fmt.Errorf("invalid forest confirmation trial id %q", trialID)
	}
	if order != spec.order {
		return fmt.Errorf("forest confirmation trial %q order %q, want %q", trialID, order, spec.order)
	}
	return nil
}

type forestAuditTrialSpec struct {
	order      string
	sequence   int
	previousID string
}

func forestAuditConfirmationTrialSpec(trialID string) (forestAuditTrialSpec, bool) {
	spec, ok := map[string]forestAuditTrialSpec{
		forestAuditConfirmationPairA:  {order: ForestAuditOrderProductionFirst, sequence: 1},
		forestAuditConfirmationPairB:  {order: ForestAuditOrderRoutedFirst, sequence: 2, previousID: forestAuditConfirmationPairA},
		forestAuditConfirmationABBAA1: {order: ForestAuditOrderProductionFirst, sequence: 3, previousID: forestAuditConfirmationPairB},
		forestAuditConfirmationABBAB1: {order: ForestAuditOrderRoutedFirst, sequence: 4, previousID: forestAuditConfirmationABBAA1},
		forestAuditConfirmationABBAB2: {order: ForestAuditOrderRoutedFirst, sequence: 5, previousID: forestAuditConfirmationABBAB1},
		forestAuditConfirmationABBAA2: {order: ForestAuditOrderProductionFirst, sequence: 6, previousID: forestAuditConfirmationABBAB2},
	}[trialID]
	return spec, ok
}

type ForestAuditConfirmationSummary struct {
	Status              string    `json:"status"`
	PooledRoutedSpeedup float64   `json:"pooled_routed_speedup,omitempty"`
	OrderSpeedups       []float64 `json:"order_speedups,omitempty"`
	RunConfigSHA256     string    `json:"run_config_sha256,omitempty"`
	CompletedTrialIDs   []string  `json:"completed_trial_ids,omitempty"`
	TrialsComplete      int       `json:"trials_complete"`
	RequiredRepeatCount int       `json:"required_repeat_count,omitempty"`
}

type ForestAuditThreeWayRelation struct {
	Classification   string   `json:"classification"`
	Paths            []string `json:"paths,omitempty"`
	EvidenceComplete bool     `json:"evidence_complete"`
}

type ForestAuditPlannedTrial struct {
	Language       string `json:"language"`
	Sequence       int    `json:"sequence"`
	TrialID        string `json:"trial_id"`
	ExecutionOrder string `json:"execution_order"`
	RepeatCount    int    `json:"repeat_count"`
	Reason         string `json:"reason"`
}

type ForestAuditConfirmationPlan struct {
	Schema               string                    `json:"schema"`
	GotreesitterRevision string                    `json:"gotreesitter_revision"`
	CorpusManifestSHA256 string                    `json:"corpus_manifest_sha256"`
	CorpusLockSHA256     string                    `json:"corpus_lock_sha256"`
	Trials               []ForestAuditPlannedTrial `json:"trials"`
}

func BuildForestAuditConfirmationPlan(report ForestAuditReport) ForestAuditConfirmationPlan {
	plan := ForestAuditConfirmationPlan{
		Schema:               ForestAuditConfirmationPlanSchema,
		GotreesitterRevision: report.GotreesitterRevision,
		CorpusManifestSHA256: report.CorpusManifestSHA256,
		CorpusLockSHA256:     report.CorpusLockSHA256,
		Trials:               []ForestAuditPlannedTrial{},
	}
	for _, row := range report.Languages {
		if !row.ScreeningEligible || row.CorrectnessRelation != forestAuditRelationProductionIdentity {
			continue
		}
		repeats := 1
		reason := row.ConfirmationStatus
		switch row.ConfirmationStatus {
		case forestAuditConfirmationRequired, "":
			if reason == "" {
				reason = forestAuditConfirmationRequired
			}
			completed := forestAuditCompletedTrialSet(row.Confirmation)
			if !completed[forestAuditConfirmationPairA] {
				plan.Trials = append(plan.Trials, ForestAuditPlannedTrial{
					Language: row.Language, Sequence: 1, TrialID: forestAuditConfirmationPairA,
					ExecutionOrder: ForestAuditOrderProductionFirst, RepeatCount: 1, Reason: reason,
				})
			}
			if !completed[forestAuditConfirmationPairB] {
				plan.Trials = append(plan.Trials, ForestAuditPlannedTrial{
					Language: row.Language, Sequence: 2, TrialID: forestAuditConfirmationPairB,
					ExecutionOrder: ForestAuditOrderRoutedFirst, RepeatCount: 1, Reason: reason,
				})
			}
		case forestAuditConfirmationReverse:
			plan.Trials = append(plan.Trials, ForestAuditPlannedTrial{Language: row.Language, Sequence: 2, TrialID: forestAuditConfirmationPairB, ExecutionOrder: ForestAuditOrderRoutedFirst, RepeatCount: 1, Reason: forestAuditConfirmationReverse})
		case forestAuditConfirmationABBA:
			if row.Confirmation != nil && row.Confirmation.RequiredRepeatCount > 1 {
				repeats = row.Confirmation.RequiredRepeatCount
			}
			completed := forestAuditCompletedTrialSet(row.Confirmation)
			for _, spec := range []struct{ id, order string }{
				{forestAuditConfirmationABBAA1, ForestAuditOrderProductionFirst},
				{forestAuditConfirmationABBAB1, ForestAuditOrderRoutedFirst},
				{forestAuditConfirmationABBAB2, ForestAuditOrderRoutedFirst},
				{forestAuditConfirmationABBAA2, ForestAuditOrderProductionFirst},
			} {
				if !completed[spec.id] {
					trialSpec, _ := forestAuditConfirmationTrialSpec(spec.id)
					plan.Trials = append(plan.Trials, ForestAuditPlannedTrial{Language: row.Language, Sequence: trialSpec.sequence, TrialID: spec.id, ExecutionOrder: spec.order, RepeatCount: repeats, Reason: reason})
				}
			}
		}
	}
	sort.Slice(plan.Trials, func(i, j int) bool {
		if plan.Trials[i].Language != plan.Trials[j].Language {
			return plan.Trials[i].Language < plan.Trials[j].Language
		}
		return plan.Trials[i].Sequence < plan.Trials[j].Sequence
	})
	return plan
}

func forestAuditCompletedTrialSet(summary *ForestAuditConfirmationSummary) map[string]bool {
	completed := make(map[string]bool)
	if summary == nil {
		return completed
	}
	for _, id := range summary.CompletedTrialIDs {
		completed[id] = true
	}
	return completed
}

func WriteForestAuditConfirmationPlan(path string, plan ForestAuditConfirmationPlan) error {
	if err := validateForestAuditConfirmationPlan(plan); err != nil {
		return err
	}
	return writeForestAuditJSON(path, plan)
}

func validateForestAuditConfirmationPlan(plan ForestAuditConfirmationPlan) error {
	if plan.Schema != ForestAuditConfirmationPlanSchema {
		return fmt.Errorf("forest confirmation plan schema %q", plan.Schema)
	}
	if err := forestManifestRevision(plan.GotreesitterRevision); err != nil {
		return err
	}
	if _, err := forestManifestDigest("manifest", plan.CorpusManifestSHA256); err != nil {
		return err
	}
	if _, err := forestManifestDigest("corpus lock", plan.CorpusLockSHA256); err != nil {
		return err
	}
	seen := make(map[string]bool, len(plan.Trials))
	for index, trial := range plan.Trials {
		if err := forestManifestLanguage(trial.Language); err != nil {
			return fmt.Errorf("confirmation plan trial %d: %w", index, err)
		}
		if err := validateForestAuditTrialID(trial.TrialID, trial.ExecutionOrder); err != nil {
			return fmt.Errorf("confirmation plan trial %d: %w", index, err)
		}
		if err := validateForestAuditRunPlan(ForestAuditRunPlan{ExecutionOrder: trial.ExecutionOrder, RepeatCount: trial.RepeatCount}); err != nil {
			return fmt.Errorf("confirmation plan trial %d: %w", index, err)
		}
		if trial.Sequence < 1 || trial.Sequence > 6 {
			return fmt.Errorf("confirmation plan trial %d has sequence %d outside 1..6", index, trial.Sequence)
		}
		spec, _ := forestAuditConfirmationTrialSpec(trial.TrialID)
		if trial.Sequence != spec.sequence {
			return fmt.Errorf("confirmation plan trial %d sequence %d, want %d", index, trial.Sequence, spec.sequence)
		}
		key := trial.Language + "\x00" + trial.TrialID
		if seen[key] {
			return fmt.Errorf("confirmation plan duplicates %s %s", trial.Language, trial.TrialID)
		}
		seen[key] = true
	}
	return nil
}

func summarizeForestAuditConfirmations(trials map[string]ForestAuditConfirmationTrial) ForestAuditConfirmationSummary {
	if len(trials) == 0 {
		return ForestAuditConfirmationSummary{Status: forestAuditConfirmationRequired}
	}
	completed := make([]string, 0, len(trials))
	for id := range trials {
		completed = append(completed, id)
	}
	sort.Strings(completed)
	if _, ok := trials[forestAuditConfirmationPairA]; !ok {
		return ForestAuditConfirmationSummary{Status: forestAuditConfirmationRequired, CompletedTrialIDs: completed, TrialsComplete: len(trials)}
	}
	if _, ok := trials[forestAuditConfirmationPairB]; !ok {
		return ForestAuditConfirmationSummary{Status: forestAuditConfirmationReverse, CompletedTrialIDs: completed, TrialsComplete: len(trials)}
	}
	pairIDs := []string{forestAuditConfirmationPairA, forestAuditConfirmationPairB}
	pair, coherent := summarizeForestAuditTrialSet(trials, pairIDs)
	pair.TrialsComplete = len(trials)
	pair.CompletedTrialIDs = completed
	if !coherent {
		pair.Status = forestAuditConfirmationInconclusive
		return pair
	}
	short := forestAuditTrialSetShort(trials, pairIDs)
	unstable := forestAuditSpeedupsUnstable(pair.OrderSpeedups)
	if pair.PooledRoutedSpeedup >= forestAuditConfirmationMinSpeedup && !short && !unstable {
		pair.Status = forestAuditConfirmationConfirmed
		return pair
	}
	if !short && forestAuditAllSpeedupsNonPositive(pair.OrderSpeedups) && !forestAuditSpeedupSpreadExceedsLimit(pair.OrderSpeedups) {
		pair.Status = forestAuditConfirmationNeutral
		return pair
	}
	pair.Status = forestAuditConfirmationABBA
	pair.RequiredRepeatCount = forestAuditRequiredRepeatCount(trials, pairIDs)

	abbaIDs := []string{forestAuditConfirmationABBAA1, forestAuditConfirmationABBAB1, forestAuditConfirmationABBAB2, forestAuditConfirmationABBAA2}
	for _, id := range abbaIDs {
		if _, ok := trials[id]; !ok {
			return pair
		}
	}
	abba, coherent := summarizeForestAuditTrialSet(trials, abbaIDs)
	abba.TrialsComplete = len(trials)
	abba.CompletedTrialIDs = completed
	if !coherent || abba.RunConfigSHA256 != pair.RunConfigSHA256 || forestAuditTrialSetShort(trials, abbaIDs) {
		abba.Status = forestAuditConfirmationInconclusive
		return abba
	}
	if forestAuditSpeedupSpreadExceedsLimit(abba.OrderSpeedups) {
		abba.Status = forestAuditConfirmationInconclusive
	} else if abba.PooledRoutedSpeedup >= forestAuditConfirmationMinSpeedup && forestAuditAllSpeedupsPositive(abba.OrderSpeedups) {
		abba.Status = forestAuditConfirmationConfirmed
	} else {
		abba.Status = forestAuditConfirmationNeutral
	}
	return abba
}

func summarizeForestAuditTrialSet(trials map[string]ForestAuditConfirmationTrial, ids []string) (ForestAuditConfirmationSummary, bool) {
	summary := ForestAuditConfirmationSummary{TrialsComplete: len(ids)}
	var peerNanos, routedNanos int64
	repeatCount := 0
	var coveredPaths []string
	for _, id := range ids {
		trial := trials[id]
		if summary.RunConfigSHA256 == "" {
			summary.RunConfigSHA256 = trial.RunConfigSHA256
		} else if summary.RunConfigSHA256 != trial.RunConfigSHA256 {
			return summary, false
		}
		if repeatCount == 0 {
			repeatCount = trial.RepeatCount
		} else if repeatCount != trial.RepeatCount {
			return summary, false
		}
		if trial.Result.Status != "pass" || trial.Result.FilesDiverged != 0 || trial.Result.FilesRoutedDiverged != 0 || trial.Result.PeerNanos <= 0 || trial.Result.RoutedNanos <= 0 {
			return summary, false
		}
		trialPaths, err := forestAuditRoutedCoveragePaths(&trial.Result)
		if err != nil {
			return summary, false
		}
		if coveredPaths == nil {
			coveredPaths = trialPaths
		} else if !forestAuditSamePaths(coveredPaths, trialPaths) {
			return summary, false
		}
		peerNanos += trial.Result.PeerNanos
		routedNanos += trial.Result.RoutedNanos
		summary.OrderSpeedups = append(summary.OrderSpeedups, float64(trial.Result.PeerNanos)/float64(trial.Result.RoutedNanos))
	}
	if routedNanos == 0 {
		return summary, false
	}
	summary.PooledRoutedSpeedup = float64(peerNanos) / float64(routedNanos)
	return summary, true
}

func forestAuditTrialSetShort(trials map[string]ForestAuditConfirmationTrial, ids []string) bool {
	for _, id := range ids {
		trial := trials[id]
		if minInt64(trial.Result.PeerNanos, trial.Result.RoutedNanos) < forestAuditConfirmationMinNanos {
			return true
		}
	}
	return false
}

func forestAuditRequiredRepeatCount(trials map[string]ForestAuditConfirmationTrial, ids []string) int {
	shortest := int64(math.MaxInt64)
	for _, id := range ids {
		trial := trials[id]
		perRepeat := minInt64(trial.Result.PeerNanos, trial.Result.RoutedNanos) / int64(trial.RepeatCount)
		if perRepeat > 0 && perRepeat < shortest {
			shortest = perRepeat
		}
	}
	if shortest == int64(math.MaxInt64) || shortest <= 0 {
		return forestAuditConfirmationMaxRepeats
	}
	repeats := int((forestAuditConfirmationMinNanos + shortest - 1) / shortest)
	if repeats < 1 {
		return 1
	}
	if repeats > forestAuditConfirmationMaxRepeats {
		return forestAuditConfirmationMaxRepeats
	}
	return repeats
}

func forestAuditSpeedupsUnstable(speedups []float64) bool {
	if len(speedups) < 2 || !forestAuditAllSpeedupsPositive(speedups) {
		return true
	}
	return forestAuditSpeedupSpreadExceedsLimit(speedups)
}

func forestAuditSpeedupSpreadExceedsLimit(speedups []float64) bool {
	if len(speedups) < 2 {
		return true
	}
	low, high := speedups[0], speedups[0]
	for _, speedup := range speedups[1:] {
		if speedup < low {
			low = speedup
		}
		if speedup > high {
			high = speedup
		}
	}
	return low <= 0 || high/low > forestAuditConfirmationMaxSpread
}

func forestAuditAllSpeedupsPositive(speedups []float64) bool {
	for _, speedup := range speedups {
		if speedup <= 1 {
			return false
		}
	}
	return len(speedups) > 0
}

func forestAuditAllSpeedupsNonPositive(speedups []float64) bool {
	for _, speedup := range speedups {
		if speedup > 1 {
			return false
		}
	}
	return len(speedups) > 0
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
