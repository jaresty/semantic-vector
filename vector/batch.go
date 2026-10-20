package vector

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const BatchVersionV1 = "semantic-vector-batch/v1"
const DeclarationUnverified = "DECLARED_UNVERIFIED"

type BatchLimits struct {
	Pair         Limits `json:"pair"`
	MaxMutations int    `json:"max_mutations"`
	// MaxTotalCanonicalBytes bounds the canonical batch snapshot: one shared valid
	// world and valid expectation plus every row's property, mutation declaration,
	// violating world and violating expectation, with shared provenance, custody,
	// ceiling, reuse declaration, and batch identity metadata encoded exactly once.
	MaxTotalCanonicalBytes int `json:"max_total_canonical_bytes"`
	MaxWork                int `json:"max_work"`
	MaxReportBytes         int `json:"max_report_bytes"`
}

func DefaultBatchLimits() BatchLimits { return BatchLimits{DefaultLimits(), 64, 4 << 20, 256, 8 << 20} }

type ValidObservationReuse struct {
	AdapterKey  string   `json:"adapter_key"`
	Scope       string   `json:"scope"`
	Assumptions []string `json:"assumptions"`
	Status      string   `json:"status"`
}
type BatchMutation[I, O, F, E any] struct{ Vector Vector[I, O, F, E] }
type BatchRun[I, O, F, E any] struct {
	Valid         Vector[I, O, F, E]
	Mutations     []BatchMutation[I, O, F, E]
	Reuse         ValidObservationReuse
	Registry      *FailureRegistry
	Limits        BatchLimits
	AdapterKey    string
	Adapter       Adapter[I, O, F, E]
	ClassifierKey string
	Classify      Classifier[O, F, E]
}

// BatchTemplate contains only genuinely shared executable dependencies and their
// custody: codecs plus the adapter/classifier binding.
type BatchTemplate[I, O, F, E any] struct {
	Codec                Codec[I]
	ObservationCodec     ObservationCodec[O, F, E]
	CodecKey             string
	ObservationCodecKey  string
	ClosureStateRetained bool
	AdapterKey           string
	Adapter              Adapter[I, O, F, E]
	ClassifierKey        string
	Classify             Classifier[O, F, E]
}

// BatchRow keeps every independently authored semantic test field and its stable
// perturbation custody key explicit.
type BatchRow[I, O, F, E any] struct {
	Property          Property
	Mutation          Perturbation[I]
	PerturbationKey   string
	ExpectedViolating Expectation[O, F, E]
}
type BatchPlan[I, O, F, E any] struct {
	Template        BatchTemplate[I, O, F, E]
	Version         string
	Oracle          Oracle
	Valid           World[I]
	ExpectedValid   Expectation[O, F, E]
	EvidenceCeiling EvidenceCeiling
	Rows            []BatchRow[I, O, F, E]
	Reuse           ValidObservationReuse
	Registry        *FailureRegistry
	Limits          BatchLimits
}

func (p BatchPlan[I, O, F, E]) run() (BatchRun[I, O, F, E], error) {
	rows := make([]BatchMutation[I, O, F, E], 0, len(p.Rows))
	keys := make(map[string]struct{}, len(p.Rows))
	ids := make(map[string]string, len(p.Rows))
	for _, r := range p.Rows {
		if strings.TrimSpace(r.PerturbationKey) == "" {
			return BatchRun[I, O, F, E]{}, Issue{Code: CodeMissing, Surface: "batch.row.perturbation_key", Detail: "stable perturbation key required"}
		}
		if _, exists := keys[r.PerturbationKey]; exists {
			return BatchRun[I, O, F, E]{}, Issue{Code: CodeInvalid, Surface: "batch.row.perturbation_key", Detail: "duplicate perturbation key"}
		}
		if prior, exists := ids[r.Mutation.ID]; exists && prior != r.PerturbationKey {
			return BatchRun[I, O, F, E]{}, Issue{Code: CodeInvalid, Surface: "batch.row.custody", Detail: "mutation identity maps to inconsistent perturbation keys"}
		}
		keys[r.PerturbationKey] = struct{}{}
		ids[r.Mutation.ID] = r.PerturbationKey
	}
	var valid Vector[I, O, F, E]
	for _, r := range p.Rows {
		custody := ExecutableCustody{CodecKey: p.Template.CodecKey, ObservationCodecKey: p.Template.ObservationCodecKey, PerturbationKey: r.PerturbationKey, ClosureStateRetained: p.Template.ClosureStateRetained}
		v := Vector[I, O, F, E]{Version: p.Version, Property: r.Property, Oracle: p.Oracle, Codec: p.Template.Codec, ObservationCodec: p.Template.ObservationCodec, Valid: p.Valid, Perturbation: r.Mutation, Executables: custody, ExpectedValid: p.ExpectedValid, ExpectedViolating: r.ExpectedViolating, EvidenceCeiling: p.EvidenceCeiling}
		rows = append(rows, BatchMutation[I, O, F, E]{v})
		if len(rows) == 1 {
			valid = v
		}
	}
	return BatchRun[I, O, F, E]{Valid: valid, Mutations: rows, Reuse: p.Reuse, Registry: p.Registry, Limits: p.Limits, AdapterKey: p.Template.AdapterKey, Adapter: p.Template.Adapter, ClassifierKey: p.Template.ClassifierKey, Classify: p.Template.Classify}, nil
}
func RunBatchPlan[I, O, F, E any](ctx context.Context, p BatchPlan[I, O, F, E]) (BatchResult, error) {
	run, err := p.run()
	if err != nil {
		return BatchResult{}, err
	}
	return RunBatchContrast(ctx, run)
}

type BatchMutationResult struct {
	MutationID      string `json:"mutation_id"`
	PerturbationKey string `json:"perturbation_key"`
	MutationDigest  string `json:"mutation_digest"`
	Report          Report `json:"report"`
}
type BatchResult struct {
	Version                string                `json:"version"`
	Identity               string                `json:"identity"`
	Valid                  WorldResult           `json:"valid"`
	Mutations              []BatchMutationResult `json:"mutations"`
	OracleVerification     VerificationStatus    `json:"oracle_verification"`
	Reuse                  ValidObservationReuse `json:"valid_observation_reuse"`
	EvidenceCeiling        EvidenceCeiling       `json:"evidence_ceiling"`
	Binding                BindingCustody        `json:"binding"`
	Limits                 BatchLimits           `json:"limits"`
	CanonicalSnapshotBytes int                   `json:"canonical_snapshot_bytes"`
}
type CompactMutationResult struct {
	MutationID      string               `json:"mutation_id"`
	PerturbationKey string               `json:"perturbation_key"`
	MutationDigest  string               `json:"mutation_digest"`
	Status          Status               `json:"status"`
	Contrast        Contrast             `json:"contrast"`
	Classification  ReportClassification `json:"classification"`
	EffectsDigest   string               `json:"effects_digest"`
	ReportDigest    string               `json:"report_digest"`
}
type CompactBatchReport struct {
	Version                string                  `json:"version"`
	BatchIdentity          string                  `json:"batch_identity"`
	FullResultDigest       string                  `json:"full_result_digest"`
	ValidStatus            bool                    `json:"valid_matched"`
	ValidDigest            string                  `json:"valid_result_digest"`
	Mutations              []CompactMutationResult `json:"mutations"`
	ProvenanceVerification VerificationStatus      `json:"provenance_verification"`
	ReuseDeclaration       ValidObservationReuse   `json:"valid_observation_reuse"`
	EvidenceCeiling        EvidenceCeiling         `json:"evidence_ceiling"`
	Limits                 BatchLimits             `json:"limits"`
	OmittedDetailClasses   []string                `json:"omitted_detail_classes"`
}

func normalizedBatchLimits(l BatchLimits) BatchLimits {
	d := DefaultBatchLimits()
	if l.Pair.MaxCanonicalBytes <= 0 || l.Pair.MaxMismatchBytes <= 0 || l.Pair.MaxRenderBytes <= 0 {
		l.Pair = d.Pair
	}
	if l.MaxMutations <= 0 {
		l.MaxMutations = d.MaxMutations
	}
	if l.MaxTotalCanonicalBytes <= 0 {
		l.MaxTotalCanonicalBytes = d.MaxTotalCanonicalBytes
	}
	if l.MaxWork <= 0 {
		l.MaxWork = d.MaxWork
	}
	if l.MaxReportBytes <= 0 {
		l.MaxReportBytes = d.MaxReportBytes
	}
	return l
}
func validReuse(r ValidObservationReuse, adapter string) bool {
	if r.AdapterKey == "" || r.AdapterKey != adapter || r.Scope == "" || r.Status != DeclarationUnverified || len(r.Assumptions) == 0 {
		return false
	}
	for _, s := range r.Assumptions {
		if strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}
func sameShared[I, O, F, E any](a, b Vector[I, O, F, E]) bool {
	return bytes.Equal(a.Valid.Canonical, b.Valid.Canonical) && fmt.Sprintf("%#v", a.Oracle) == fmt.Sprintf("%#v", b.Oracle) && fmt.Sprintf("%#v", a.EvidenceCeiling) == fmt.Sprintf("%#v", b.EvidenceCeiling) && a.Executables.CodecKey == b.Executables.CodecKey && a.Executables.ObservationCodecKey == b.Executables.ObservationCodecKey
}

type batchPrepared[I, O, F, E any] struct {
	items    []batchPreparedItem[I, O, F, E]
	snapshot []byte
	identity string
	limits   BatchLimits
}
type batchPreparedItem[I, O, F, E any] struct {
	id, digest string
	frozen     FrozenDataSnapshot[I, O, F, E]
}

func prepareBatch[I, O, F, E any](run BatchRun[I, O, F, E], enforce bool) (batchPrepared[I, O, F, E], error) {
	var out batchPrepared[I, O, F, E]
	l := normalizedBatchLimits(run.Limits)
	out.limits = l
	if run.Mutations == nil || len(run.Mutations) == 0 {
		return out, Issue{Code: CodeMissing, Surface: "batch.mutations", Detail: "nonempty mutation list required"}
	}
	if len(run.Mutations) > l.MaxMutations || len(run.Mutations)+1 > l.MaxWork {
		return out, Issue{Code: CodeOverflow, Surface: "batch.limit", Detail: "mutation or work limit exceeded"}
	}
	if !validReuse(run.Reuse, run.AdapterKey) {
		return out, Issue{Code: CodeMissing, Surface: "batch.valid_observation_reuse", Detail: "explicit DECLARED_UNVERIFIED reuse declaration bound to adapter required"}
	}
	ordered := append([]BatchMutation[I, O, F, E](nil), run.Mutations...)
	ids := map[string]bool{}
	for _, m := range ordered {
		id := m.Vector.Perturbation.ID
		if id == "" {
			return out, Issue{Code: CodeMissing, Surface: "batch.mutation.id", Detail: "immutable mutation id required"}
		}
		if ids[id] {
			return out, Issue{Code: CodeInvalid, Surface: "batch.duplicate", Detail: "duplicate mutation identity"}
		}
		ids[id] = true
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Vector.Perturbation.ID < ordered[j].Vector.Perturbation.ID })
	out.items = make([]batchPreparedItem[I, O, F, E], 0, len(ordered))
	digests := map[string]bool{}
	for _, m := range ordered {
		v, e := applyContrastDefaults(ContrastSpec[I, O, F, E]{Vector: m.Vector})
		if e != nil {
			return out, e
		}
		if !sameShared(run.Valid, v.Vector) {
			return out, Issue{Code: CodeInvalid, Surface: "batch.shared", Detail: "mixed valid world, provenance, ceiling, or codec custody"}
		}
		f, e := FreezeData(v.Vector, run.Registry, l.Pair)
		if e != nil {
			return out, e
		}
		if len(out.items) == 0 && (!bytes.Equal(run.Valid.ExpectedValid.Canonical.Output, f.prepared.validExpected.Output) || !bytes.Equal(run.Valid.ExpectedValid.Canonical.Failure, f.prepared.validExpected.Failure) || !bytes.Equal(run.Valid.ExpectedValid.Canonical.Effects, f.prepared.validExpected.Effects) || run.Valid.ExpectedValid.Classification != f.vector.ExpectedValid.Classification || fmt.Sprintf("%#v", run.Valid.ExpectedValid.FailureExpectation) != fmt.Sprintf("%#v", f.vector.ExpectedValid.FailureExpectation)) {
			return out, Issue{Code: CodeInvalid, Surface: "batch.shared", Detail: "declared shared valid expectation differs from mutation rows"}
		}
		if len(out.items) > 0 {
			first := out.items[0].frozen
			if !bytes.Equal(first.prepared.validExpected.Output, f.prepared.validExpected.Output) || !bytes.Equal(first.prepared.validExpected.Failure, f.prepared.validExpected.Failure) || !bytes.Equal(first.prepared.validExpected.Effects, f.prepared.validExpected.Effects) || first.vector.ExpectedValid.Classification != f.vector.ExpectedValid.Classification || fmt.Sprintf("%#v", first.vector.ExpectedValid.FailureExpectation) != fmt.Sprintf("%#v", f.vector.ExpectedValid.FailureExpectation) {
				return out, Issue{Code: CodeInvalid, Surface: "batch.shared", Detail: "mixed valid-world expectations"}
			}
		}
		dg := digest(f.prepared.violatingBytes)
		if digests[dg] {
			return out, Issue{Code: CodeInvalid, Surface: "batch.duplicate", Detail: "duplicate violating-world digest"}
		}
		digests[dg] = true
		out.items = append(out.items, batchPreparedItem[I, O, F, E]{v.Vector.Perturbation.ID, dg, f})
	}
	first := out.items[0].frozen
	type row struct {
		Property        Property             `json:"property"`
		MutationID      string               `json:"mutation_id"`
		Difference      WholeWorldDifference `json:"difference"`
		PerturbationKey string               `json:"perturbation_key"`
		Violating       []byte               `json:"violating"`
		Expected        CanonicalExpectation `json:"expected_violating"`
		Failure         FailureExpectation   `json:"failure_expectation"`
		Classification  string               `json:"classification"`
	}
	snap := struct {
		Version             string                `json:"version"`
		Valid               []byte                `json:"valid"`
		ExpectedValid       CanonicalExpectation  `json:"expected_valid"`
		ValidFailure        FailureExpectation    `json:"valid_failure_expectation"`
		ValidClassification string                `json:"valid_classification"`
		Oracle              Oracle                `json:"oracle"`
		CodecKey            string                `json:"codec_key"`
		ObservationCodecKey string                `json:"observation_codec_key"`
		AdapterKey          string                `json:"adapter_key"`
		ClassifierKey       string                `json:"classifier_key"`
		Ceiling             EvidenceCeiling       `json:"evidence_ceiling"`
		Reuse               ValidObservationReuse `json:"valid_observation_reuse"`
		Rows                []row                 `json:"rows"`
	}{BatchVersionV1, first.prepared.validBytes, first.prepared.validExpected, first.vector.ExpectedValid.FailureExpectation, first.vector.ExpectedValid.Classification, first.vector.Oracle, first.vector.Executables.CodecKey, first.vector.Executables.ObservationCodecKey, run.AdapterKey, run.ClassifierKey, first.vector.EvidenceCeiling, run.Reuse, make([]row, 0, len(out.items))}
	for _, x := range out.items {
		f := x.frozen
		snap.Rows = append(snap.Rows, row{f.vector.Property, x.id, f.vector.Perturbation.Difference, f.vector.Executables.PerturbationKey, f.prepared.violatingBytes, f.prepared.violatingExpected, f.vector.ExpectedViolating.FailureExpectation, f.vector.ExpectedViolating.Classification})
	}
	b, e := json.Marshal(snap)
	if e != nil {
		return out, e
	}
	if enforce && len(b) > l.MaxTotalCanonicalBytes {
		return out, Issue{Code: CodeOverflow, Surface: "batch.canonical_snapshot", Detail: "canonical batch snapshot exceeds MaxTotalCanonicalBytes"}
	}
	out.snapshot = append([]byte(nil), b...)
	out.identity = digest(append([]byte("semantic-vector-batch/v1\x00"), b...))
	return out, nil
}
func BatchCanonicalSnapshotSize[I, O, F, E any](run BatchRun[I, O, F, E]) (int, error) {
	p, e := prepareBatch(run, false)
	return len(p.snapshot), e
}

func RunBatchContrast[I, O, F, E any](ctx context.Context, run BatchRun[I, O, F, E]) (BatchResult, error) {
	p, err := prepareBatch(run, true)
	if err != nil {
		return BatchResult{}, err
	}
	first := p.items[0].frozen
	out := BatchResult{Version: BatchVersionV1, Identity: p.identity, Mutations: make([]BatchMutationResult, 0, len(p.items)), OracleVerification: first.vector.Oracle.VerificationStatus(), Reuse: ValidObservationReuse{run.Reuse.AdapterKey, run.Reuse.Scope, append([]string{}, run.Reuse.Assumptions...), run.Reuse.Status}, EvidenceCeiling: first.vector.EvidenceCeiling, Binding: BindingCustody{AdapterKey: run.AdapterKey, ClassifierKey: run.ClassifierKey}, Limits: p.limits, CanonicalSnapshotBytes: len(p.snapshot)}
	base, err := BindTrusted(first, run.AdapterKey, run.Adapter, run.ClassifierKey, run.Classify)
	if err != nil {
		return BatchResult{}, err
	}
	eev, err := expectedEncoded(first.prepared.validExpected, p.limits.Pair)
	if err != nil {
		return BatchResult{}, err
	}
	out.Valid, err = base.execute(ctx, "valid", first.prepared.validBytes, first.vector.ExpectedValid, eev)
	if err != nil {
		return BatchResult{}, err
	}
	for _, x := range p.items {
		bound, e := BindTrusted(x.frozen, run.AdapterKey, run.Adapter, run.ClassifierKey, run.Classify)
		if e != nil {
			return BatchResult{}, e
		}
		eex, e := expectedEncoded(x.frozen.prepared.violatingExpected, p.limits.Pair)
		if e != nil {
			return BatchResult{}, e
		}
		violating, e := bound.execute(ctx, "violating", x.frozen.prepared.violatingBytes, x.frozen.vector.ExpectedViolating, eex)
		rep := newReport(x.frozen.vector, p.limits.Pair)
		rep.VectorID, rep.ReproductionID, rep.Binding = x.frozen.Identity(), x.frozen.Identity(), bound.custody
		rep.Valid = out.Valid
		if e != nil {
			rep.Findings = append(rep.Findings, issueFrom(e, CodeInvalid, "violating", "execution"))
		} else {
			rep.Violating = violating
			rep.Findings = append(rep.Findings, out.Valid.Findings...)
			rep.Findings = append(rep.Findings, violating.Findings...)
			if out.Valid.Classification == violating.Classification {
				rep.Contrast = ContrastAbsent
				rep.Findings = append(rep.Findings, Finding{Code: CodeNondiscriminating, Surface: "classification", Detail: "observed classifications are identical"})
			} else if out.Valid.Classification == x.frozen.vector.ExpectedValid.Classification && violating.Classification == x.frozen.vector.ExpectedViolating.Classification {
				rep.Contrast = ContrastEstablished
			}
		}
		rep = finishReport(rep)
		out.Mutations = append(out.Mutations, BatchMutationResult{MutationID: x.id, PerturbationKey: x.frozen.vector.Executables.PerturbationKey, MutationDigest: x.digest, Report: rep})
	}
	return out, nil
}

func (r BatchResult) Structured() ([]byte, error) {
	if r.Mutations == nil {
		return nil, Issue{Code: CodeMissing, Surface: "batch.result.mutations", Detail: "required list is nil"}
	}
	b, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	limit := r.Limits.MaxReportBytes
	if limit <= 0 {
		limit = DefaultBatchLimits().MaxReportBytes
	}
	if len(b) > limit {
		return nil, Issue{Code: CodeOverflow, Surface: "batch.report", Detail: "full batch report exceeds limit"}
	}
	return b, nil
}

type compactWire struct {
	V  string                `json:"v"`
	I  string                `json:"i"`
	F  string                `json:"f"`
	VM bool                  `json:"vm"`
	VD string                `json:"vd"`
	M  [][]any               `json:"m"`
	D  []string              `json:"d"`
	P  VerificationStatus    `json:"p"`
	R  ValidObservationReuse `json:"r"`
	C  EvidenceCeiling       `json:"c"`
	L  BatchLimits           `json:"l"`
	O  []string              `json:"o"`
}

func classificationBits(c ReportClassification) int {
	n := 0
	if c.ExpectationMismatch {
		n |= 1
	}
	if c.ExecutionFailure {
		n |= 2
	}
	if c.TestDefinitionDefect {
		n |= 4
	}
	return n
}
func compactProjection(r BatchResult, full []byte) CompactBatchReport {
	vb, _ := json.Marshal(r.Valid)
	c := CompactBatchReport{BatchVersionV1, r.Identity, digest(full), r.Valid.Matched, digest(vb), make([]CompactMutationResult, 0, len(r.Mutations)), r.OracleVerification, r.Reuse, r.EvidenceCeiling, r.Limits, []string{"canonical observations", "finding details", "oracle evidence", "property metadata", "per-pair valid world", "per-pair report digest", "reproduction and binding detail"}}
	for _, m := range r.Mutations {
		c.Mutations = append(c.Mutations, CompactMutationResult{MutationID: m.MutationID, PerturbationKey: m.PerturbationKey, MutationDigest: m.MutationDigest, Status: m.Report.Status, Contrast: m.Report.Contrast, Classification: m.Report.Classification, EffectsDigest: digest([]byte(m.Report.Violating.Observation.Effects))})
	}
	return c
}
func compactBytes(c CompactBatchReport) ([]byte, error) {
	w := compactWire{V: c.Version, I: c.BatchIdentity, F: c.FullResultDigest, VM: c.ValidStatus, VD: c.ValidDigest, M: make([][]any, 0, len(c.Mutations)), D: []string{}, P: c.ProvenanceVerification, R: c.ReuseDeclaration, C: c.EvidenceCeiling, L: c.Limits, O: c.OmittedDetailClasses}
	idx := map[string]int{}
	for _, m := range c.Mutations {
		di, ok := idx[m.EffectsDigest]
		if !ok {
			di = len(w.D)
			idx[m.EffectsDigest] = di
			w.D = append(w.D, m.EffectsDigest)
		}
		w.M = append(w.M, []any{m.MutationID, m.PerturbationKey, m.MutationDigest, string(m.Status), string(m.Contrast), classificationBits(m.Classification), string(m.Classification.Discrimination), string(m.Classification.Attribution), di})
	}
	return json.Marshal(w)
}
func (r BatchResult) Compact() ([]byte, error) {
	full, e := r.Structured()
	if e != nil {
		return nil, e
	}
	return compactBytes(compactProjection(r, full))
}

func rejectDuplicateJSONKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				kt, e := d.Token()
				if e != nil {
					return e
				}
				k, ok := kt.(string)
				if !ok || seen[k] {
					return fmt.Errorf("duplicate object key")
				}
				seen[k] = true
				if e = walk(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case json.Delim('['):
			for d.More() {
				if e := walk(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		default:
			return nil
		}
	}
	if e := walk(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}
func validDigest(s string) bool {
	if !strings.HasPrefix(s, "sha256:") || len(s) != 71 {
		return false
	}
	_, e := hex.DecodeString(s[7:])
	return e == nil
}
func validLimits(l BatchLimits) bool {
	return l.Pair.MaxCanonicalBytes > 0 && l.Pair.MaxMismatchBytes > 0 && l.Pair.MaxRenderBytes > 0 && l.MaxMutations > 0 && l.MaxTotalCanonicalBytes > 0 && l.MaxWork > 0 && l.MaxReportBytes > 0
}
func validCeiling(c EvidenceCeiling) bool {
	if strings.TrimSpace(c.Version) == "" || len(c.CannotEstablish) == 0 {
		return false
	}
	for _, s := range c.CannotEstablish {
		if strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}
func ParseCompactBatch(data []byte) (CompactBatchReport, error) {
	var c CompactBatchReport
	if e := rejectDuplicateJSONKeys(data); e != nil {
		return c, Issue{Code: CodeInvalid, Surface: "batch.compact", Detail: "invalid, duplicate, or trailing JSON"}
	}
	var w compactWire
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(&w); e != nil {
		return c, Issue{Code: CodeInvalid, Surface: "batch.compact", Detail: "strict decode failed"}
	}
	if w.V != BatchVersionV1 || !validDigest(w.I) || !validDigest(w.F) || !validDigest(w.VD) || w.M == nil || w.D == nil || len(w.O) == 0 || !validReuse(w.R, w.R.AdapterKey) || !validCeiling(w.C) || !validLimits(w.L) || (w.P != VerificationUnverified && w.P != VerificationIndependentlyReviewed) {
		return c, Issue{Code: CodeInvalid, Surface: "batch.compact", Detail: "semantic shape invalid"}
	}
	for _, s := range w.O {
		if strings.TrimSpace(s) == "" {
			return c, Issue{Code: CodeInvalid, Surface: "batch.compact.omissions", Detail: "empty omission"}
		}
	}
	for _, dg := range w.D {
		if !validDigest(dg) {
			return c, Issue{Code: CodeInvalid, Surface: "batch.compact.digests", Detail: "invalid digest"}
		}
	}
	c = CompactBatchReport{w.V, w.I, w.F, w.VM, w.VD, make([]CompactMutationResult, 0, len(w.M)), w.P, w.R, w.C, w.L, w.O}
	ids := map[string]bool{}
	keys := map[string]bool{}
	last := ""
	for _, x := range w.M {
		if len(x) != 9 {
			return c, Issue{Code: CodeInvalid, Surface: "batch.compact.mutations", Detail: "mutation tuple width"}
		}
		s := func(i int) (string, bool) { v, ok := x[i].(string); return v, ok }
		id, ok := s(0)
		pk, pok := s(1)
		md, mok := s(2)
		st, sok := s(3)
		co, cok := s(4)
		bits, bok := x[5].(float64)
		di, dok := s(6)
		at, aok := s(7)
		ix, iok := x[8].(float64)
		if !ok || id == "" || !pok || pk == "" || ids[id] || keys[pk] || (last != "" && id <= last) || !mok || !validDigest(md) || !sok || (Status(st) != Pass && Status(st) != Failed) || !cok || (Contrast(co) != ContrastEstablished && Contrast(co) != ContrastAbsent && Contrast(co) != ContrastInvalid) || !bok || bits != float64(int(bits)) || bits < 0 || bits > 7 || !dok || Contrast(di) != Contrast(co) || !aok || Attribution(at) != AttributionUnresolved || !iok || ix != float64(int(ix)) || int(ix) < 0 || int(ix) >= len(w.D) {
			return c, Issue{Code: CodeInvalid, Surface: "batch.compact.mutations", Detail: "invalid mutation semantics"}
		}
		ids[id] = true
		keys[pk] = true
		last = id
		b := int(bits)
		cl := ReportClassification{b&1 != 0, b&2 != 0, b&4 != 0, Contrast(di), Attribution(at)}
		c.Mutations = append(c.Mutations, CompactMutationResult{MutationID: id, PerturbationKey: pk, MutationDigest: md, Status: Status(st), Contrast: Contrast(co), Classification: cl, EffectsDigest: w.D[int(ix)]})
	}
	canonical, e := compactBytes(c)
	if e != nil {
		return c, e
	}
	if !bytes.Equal(canonical, data) {
		return c, Issue{Code: CodeInvalid, Surface: "batch.compact", Detail: "noncanonical compact bytes"}
	}
	return c, nil
}
func decodeFullBatch(data []byte) (BatchResult, error) {
	var r BatchResult
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(&r); e != nil {
		return r, Issue{Code: CodeInvalid, Surface: "batch.full", Detail: "strict decode failed"}
	}
	var trailing any
	if e := d.Decode(&trailing); e != io.EOF {
		return r, Issue{Code: CodeInvalid, Surface: "batch.full", Detail: "trailing data"}
	}
	b, e := json.Marshal(r)
	if e != nil || !bytes.Equal(b, data) {
		return r, Issue{Code: CodeInvalid, Surface: "batch.full", Detail: "noncanonical full bytes"}
	}
	return r, nil
}
func VerifyCompactBatch(fullBytes, compact []byte) error {
	r, e := decodeFullBatch(fullBytes)
	if e != nil {
		return e
	}
	if _, e = ParseCompactBatch(compact); e != nil {
		return e
	}
	expected, e := compactBytes(compactProjection(r, fullBytes))
	if e != nil {
		return e
	}
	if !bytes.Equal(expected, compact) {
		return Issue{Code: CodeInvalid, Surface: "batch.compact.binding", Detail: "compact report does not relationally match full batch result"}
	}
	return nil
}
func (r BatchResult) VerifyCompact(compact []byte) error {
	full, e := r.Structured()
	if e != nil {
		return e
	}
	return VerifyCompactBatch(full, compact)
}
