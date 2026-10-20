package vector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCanonicalVersionsRejectSilentReinterpretation(t *testing.T) {
	if VersionV0 != "semantic-vector/v0" || VersionV1 != "semantic-vector/v1" || Version != VersionV1 {
		t.Fatalf("versions: %q %q %q", VersionV0, VersionV1, Version)
	}
	v, _ := base(t)
	v.Version = VersionV0
	if _, err := FreezeData(v, nil, DefaultLimits()); !errors.Is(err, ErrLegacyVersion) {
		t.Fatalf("v0 accepted by v1 freeze: %v", err)
	}
	if _, err := (Runner[input, string, string, string]{Limits: DefaultLimits()}).Identity(v); !errors.Is(err, ErrLegacyVersion) {
		t.Fatalf("v0 accepted by v1 identity: %v", err)
	}
}

func historicalV0Vector(t *testing.T) Vector[input, string, string, string] {
	t.Helper()
	observations := &obsCodec{}
	valid := input{Allowed: true, Tags: []string{"original"}}
	violating := input{Allowed: false, Tags: []string{"original"}}
	validBytes, violatingBytes := canon(t, valid), canon(t, violating)
	return Vector[input, string, string, string]{Version: VersionV0, Property: Property{"allow", "allowed requests succeed", "contract", "1"}, Oracle: Oracle{Kind: "reference", Source: "contract", Revision: "1", Producer: "reviewer", Method: "manual", Review: "reviewed", Independence: Independent}, Codec: jsonCodec[input]{}, ObservationCodec: observations, Valid: World[input]{Canonical: validBytes}, Perturbation: Perturbation[input]{ID: "deny", Difference: WholeWorldDifference{BeforeDigest: digest(validBytes), AfterDigest: digest(violatingBytes)}, Apply: func(i input) (input, error) { i.Allowed = false; return i, nil }}, ExpectedValid: expectation(t, observations, "ok", "", "allowed", []string{"write"}), ExpectedViolating: expectation(t, observations, "", "denied", "denied", nil), EvidenceCeiling: EvidenceCeiling{"v0", []string{"semantic completeness"}}}
}

func TestV0GoldenIdentityCompatibility(t *testing.T) {
	v := historicalV0Vector(t)
	got, err := LegacyV0Identity(v, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/v0/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Identity string `json:"identity"`
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Identity == "PENDING_GOLDEN" {
		t.Fatalf("record golden identity: %s", got)
	}
	if got != golden.Identity {
		t.Fatalf("v0 identity changed: %s != %s", got, golden.Identity)
	}
}

func TestLegacyV0RejectsV1FieldsAndHistoricalInvalidity(t *testing.T) {
	for name, mutate := range map[string]func(*Vector[input, string, string, string]){
		"verification": func(v *Vector[input, string, string, string]) { v.Oracle.Verification = reviewedEvidence() },
		"failure expectation": func(v *Vector[input, string, string, string]) {
			v.ExpectedViolating.FailureExpectation = FailureExpectation{Class: "denied"}
		},
		"executable custody": func(v *Vector[input, string, string, string]) { v.Executables = testExecutableCustody() },
	} {
		t.Run(name, func(t *testing.T) {
			v := historicalV0Vector(t)
			mutate(&v)
			if _, err := LegacyV0Identity(v, DefaultLimits()); !errors.Is(err, ErrLegacyVersion) {
				t.Fatalf("v1 field accepted: %v", err)
			}
		})
	}
	v := historicalV0Vector(t)
	v.ExpectedViolating.Classification = v.ExpectedValid.Classification
	if _, err := LegacyV0Identity(v, DefaultLimits()); err == nil {
		t.Fatal("historically invalid classification accepted")
	}
}

func TestV0GoldenReportCompatibility(t *testing.T) {
	report := Report{Version: VersionV0, VectorID: "sha256:vector", PropertyID: "p", Status: Pass, Contrast: ContrastEstablished, Oracle: Oracle{Kind: "reference", Source: "contract", Revision: "1", Producer: "reviewer", Method: "manual", Review: "reviewed", Independence: Reference}, OracleQualified: true, EvidenceCeiling: EvidenceCeiling{Version: "v0", CannotEstablish: []string{"domain truth"}}, ReproductionID: "sha256:vector"}
	encoded, err := LegacyV0StructuredReport(report)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/v0/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	got := digest(encoded)
	if golden.Digest == "PENDING_GOLDEN" {
		t.Fatalf("record golden report digest: %s", got)
	}
	if got != golden.Digest {
		t.Fatalf("v0 report changed: %s != %s", got, golden.Digest)
	}
}

func TestLegacyV0ReportRejectsV1AttributionClaim(t *testing.T) {
	report := Report{Version: VersionV0, AttributionClaim: &AttributionClaim{ClaimedAttribution: AttributionImplementationDefect, Status: AttributionClaimDeclaredUnverified, DeclaredEvidence: []string{"self claim"}}}
	if _, err := LegacyV0StructuredReport(report); !errors.Is(err, ErrLegacyVersion) {
		t.Fatalf("v1 report claim accepted as v0: %v", err)
	}
}

func TestLegacyV0ReportRejectsOracleVerification(t *testing.T) {
	report := Report{Version: VersionV0, Oracle: Oracle{Verification: reviewedEvidence()}}
	if _, err := LegacyV0StructuredReport(report); !errors.Is(err, ErrLegacyVersion) {
		t.Fatalf("v1 oracle verification accepted as v0: %v", err)
	}
}

func TestLegacyV0ReportRejectsEachV1OnlyFieldIndependently(t *testing.T) {
	cases := []struct {
		name   string
		report Report
	}{
		{name: "oracle verification status only", report: Report{Version: VersionV0, Oracle: Oracle{Verification: Verification{Status: VerificationIndependentlyReviewed}}}},
		{name: "oracle verification evidence only", report: Report{Version: VersionV0, Oracle: Oracle{Verification: Verification{Evidence: reviewedEvidence().Evidence}}}},
		{name: "classification only", report: Report{Version: VersionV0, Classification: ReportClassification{Attribution: AttributionUnresolved}}},
		{name: "binding only", report: Report{Version: VersionV0, Binding: BindingCustody{AdapterKey: "adapter/v1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LegacyV0StructuredReport(tc.report); !errors.Is(err, ErrLegacyVersion) {
				t.Fatalf("independent v1-only field accepted as v0: %v", err)
			}
		})
	}
}

func TestV1IdentityDomainSeparation(t *testing.T) {
	v, _ := base(t)
	v.Version = VersionV1
	a, err := FreezeData(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	v.Oracle.Verification.Evidence[0].Scope = "different bounded scope"
	b, err := FreezeData(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if a.Identity() == b.Identity() {
		t.Fatal("v1 verification domain omitted from identity")
	}
}

func TestFrozenDataSnapshotDeepCopiesCanonicalDataAliases(t *testing.T) {
	v, _ := base(t)
	snapshot, err := FreezeData(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	v.Valid.Canonical[0] ^= 0xff
	v.ExpectedValid.Canonical.Output[0] ^= 0xff
	v.ExpectedValid.Effects.Values[0] = "changed"
	v.EvidenceCeiling.CannotEstablish[0] = "changed"
	v.Oracle.Verification.Evidence[0].Scope = "changed"
	if snapshot.vector.ExpectedValid.Effects.Values[0] == "changed" || snapshot.vector.EvidenceCeiling.CannotEstablish[0] == "changed" || snapshot.vector.Oracle.Verification.Evidence[0].Scope == "changed" {
		t.Fatal("caller-owned data alias reached snapshot")
	}
}

func TestFrozenDataSnapshotRetainsExecutableClosureState(t *testing.T) {
	v, _ := base(t)
	var deny atomic.Bool
	v.Perturbation.Apply = func(i input) (input, error) { i.Allowed = !deny.Load(); return i, nil }
	deny.Store(true)
	bad := input{Allowed: false, Tags: []string{"original"}}
	v.Perturbation.Difference.AfterDigest = digest(canon(t, bad))
	snapshot, err := FreezeData(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	deny.Store(false)
	if snapshot.ExecutableCustody().PerturbationKey == "" {
		t.Fatal("missing perturbation custody key")
	}
	if !snapshot.ExecutableCustody().ClosureStateRetained {
		t.Fatal("closure limitation not represented")
	}
}

func TestMutableMatcherClosureRemainsMutableAfterSnapshot(t *testing.T) {
	v, _ := base(t)
	v.ExpectedViolating.FailureExpectation = FailureExpectation{MatcherKey: "mutable/v1"}
	var accepts atomic.Bool
	accepts.Store(true)
	registry := NewFailureRegistry()
	if err := registry.Register("mutable/v1", func(error) bool { return accepts.Load() }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := FreezeData(v, registry, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	matcher := snapshot.matcher("mutable/v1")
	if ok, err := invokeMatcher(matcher, errDenied); err != nil || !ok {
		t.Fatalf("before mutation: %v %v", ok, err)
	}
	accepts.Store(false)
	if ok, err := invokeMatcher(matcher, errDenied); err != nil || ok {
		t.Fatalf("closure state was falsely frozen: %v %v", ok, err)
	}
}

type errorObservationCodec struct{}

func (errorObservationCodec) EncodeOutput(v string) ([]byte, error) { return json.Marshal(v) }
func (errorObservationCodec) EncodeFailure(v error) ([]byte, error) {
	if v == nil {
		return json.Marshal("")
	}
	return json.Marshal(v.Error())
}
func (errorObservationCodec) EncodeEffects(v []string) ([]byte, error) {
	return json.Marshal(normalized(v))
}

type errorAdapter struct{}

func (errorAdapter) Execute(_ context.Context, in input) (Observation[string, error, string], error) {
	if in.Allowed {
		return Observation[string, error, string]{Output: "ok", Effects: []string{"write"}}, nil
	}
	return Observation[string, error, string]{Failure: classifiedError{class: "permission-denied", msg: "denied", err: errDenied}, Effects: []string{}}, nil
}

func TestMatcherPanicBecomesExecutionFailureReport(t *testing.T) {
	codec := errorObservationCodec{}
	valid, violating := input{Allowed: true}, input{Allowed: false}
	vb, bb := canon(t, valid), canon(t, violating)
	outOK, _ := codec.EncodeOutput("ok")
	outZero, _ := codec.EncodeOutput("")
	failZero, _ := codec.EncodeFailure(nil)
	expectedFailure := classifiedError{class: "permission-denied", msg: "denied", err: errDenied}
	failDenied, _ := codec.EncodeFailure(expectedFailure)
	write, _ := codec.EncodeEffects([]string{"write"})
	none, _ := codec.EncodeEffects([]string{})
	v := Vector[input, string, error, string]{Version: VersionV1, Property: Property{ID: "p", Statement: "deny invalid", Source: "contract"}, Oracle: Oracle{Kind: "reference", Source: "contract", Revision: "1", Producer: "author", Method: "manual", Review: "evidence", Independence: Reference, Verification: reviewedEvidence()}, Codec: jsonCodec[input]{}, ObservationCodec: codec, Valid: World[input]{Canonical: vb}, Perturbation: Perturbation[input]{ID: "deny", Difference: WholeWorldDifference{BeforeDigest: digest(vb), AfterDigest: digest(bb)}, Apply: func(i input) (input, error) { i.Allowed = false; return i, nil }}, Executables: testExecutableCustody(), ExpectedValid: Expectation[string, error, string]{Output: "ok", Effects: Effects[string]{Known: true, Values: []string{"write"}}, Classification: "allowed", Canonical: CanonicalExpectation{Output: outOK, Failure: failZero, Effects: write}}, ExpectedViolating: Expectation[string, error, string]{Failure: expectedFailure, Effects: NoEffects[string](), Classification: "denied", Canonical: CanonicalExpectation{Output: outZero, Failure: failDenied, Effects: none}, FailureExpectation: FailureExpectation{Class: "permission-denied", MatcherKey: "panic/v1"}}, EvidenceCeiling: EvidenceCeiling{Version: "v1", CannotEstablish: []string{"domain truth"}}}
	registry := NewFailureRegistry()
	if err := registry.Register("panic/v1", func(error) bool { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := FreezeData(v, registry, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	classify := func(o Observation[string, error, string]) (string, error) {
		if o.Failure != nil {
			return "denied", nil
		}
		return "allowed", nil
	}
	bound, err := BindTrusted(snapshot, "adapter/error/v1", errorAdapter{}, "classifier/error/v1", classify)
	if err != nil {
		t.Fatal(err)
	}
	report := bound.Run(context.Background())
	if !has(report, CodeMatcherPanic) || !report.Classification.ExecutionFailure || report.Classification.Attribution != AttributionUnresolved {
		t.Fatalf("%#v %#v", report.Findings, report.Classification)
	}
}

func TestMatcherPanicContained(t *testing.T) {
	r := NewFailureRegistry()
	if err := r.Register("panic/v1", func(error) bool { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	_, err := (FailureExpectation{MatcherKey: "panic/v1"}).MatchResult(errDenied, r)
	var issue Issue
	if !errors.As(err, &issue) || issue.Code != CodeMatcherPanic {
		t.Fatalf("panic not typed: %v", err)
	}
}

func TestRegistryReplacementDoesNotChangeSnapshotAndConcurrentMatchIsSafe(t *testing.T) {
	v, _ := base(t)
	v.ExpectedViolating.FailureExpectation = FailureExpectation{MatcherKey: "denied/v1"}
	r := NewFailureRegistry()
	if err := r.Register("denied/v1", func(error) bool { return true }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := FreezeData(v, r, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Replace("denied/v1", func(error) bool { return false }); err != nil {
		t.Fatal(err)
	}
	matcher := snapshot.matcher("denied/v1")
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := invokeMatcher(matcher, errDenied)
			if err != nil || !ok {
				t.Errorf("snapshot matcher changed: %v %v", ok, err)
			}
		}()
	}
	wg.Wait()
}

func TestReportAttributionTruthTable(t *testing.T) {
	cases := []struct {
		name                                        string
		findings                                    []Finding
		wantMismatch, wantExecution, wantDefinition bool
	}{
		{"mismatch", []Finding{{Code: CodeMismatch}}, true, false, false},
		{"adapter", []Finding{{Code: CodeAdapter}}, false, true, false},
		{"definition", []Finding{{Code: CodeInvalid}}, false, false, true},
		{"nondiscriminating", []Finding{{Code: CodeNondiscriminating}}, false, false, false},
	}
	for _, tc := range cases {
		r := finishReport(Report{Contrast: ContrastInvalid, Findings: tc.findings})
		if r.Classification.ExpectationMismatch != tc.wantMismatch || r.Classification.ExecutionFailure != tc.wantExecution || r.Classification.TestDefinitionDefect != tc.wantDefinition || r.Classification.Attribution != AttributionUnresolved {
			t.Fatalf("%s: %#v", tc.name, r.Classification)
		}
	}
}

func TestDeclaredAttributionCannotUpgradeAuthorityOrChangeResult(t *testing.T) {
	r := finishReport(Report{Version: VersionV1, VectorID: "sha256:vector", Status: Failed, Contrast: ContrastInvalid, Findings: []Finding{{Code: CodeMismatch}}})
	beforeStatus, beforeContrast, beforeFindings, beforeVectorID := r.Status, r.Contrast, append([]Finding(nil), r.Findings...), r.VectorID
	out, err := DeclareAttribution(r, AttributionImplementationDefect, "caller says implementation")
	if err != nil {
		t.Fatal(err)
	}
	if out.Classification.Attribution != AttributionUnresolved || out.AttributionClaim == nil || out.AttributionClaim.Status != AttributionClaimDeclaredUnverified || out.AttributionClaim.ClaimedAttribution != AttributionImplementationDefect {
		t.Fatalf("claim changed authority: %#v %#v", out.Classification, out.AttributionClaim)
	}
	if out.Status != beforeStatus || out.Contrast != beforeContrast || out.VectorID != beforeVectorID || len(out.Findings) != len(beforeFindings) {
		t.Fatalf("claim changed report result: %#v", out)
	}
	out.AttributionClaim.DeclaredEvidence[0] = "mutated"
	if r.AttributionClaim != nil {
		t.Fatal("claim aliased original report")
	}
}

func TestSelfClaimStringsNeverBecomeReviewedOrVerified(t *testing.T) {
	r := finishReport(Report{Findings: []Finding{{Code: CodeMismatch}}})
	out, err := Adjudicate(r, Adjudication{Attribution: AttributionTestDefinitionDefect, Evidence: reviewedEvidence().Evidence})
	if err != nil {
		t.Fatal(err)
	}
	if out.Classification.Attribution != AttributionUnresolved || out.AttributionClaim == nil || out.AttributionClaim.Status != AttributionClaimDeclaredUnverified {
		t.Fatalf("self-claim upgraded: %#v %#v", out.Classification, out.AttributionClaim)
	}
	if _, err := DeclareAttribution(r, AttributionImplementationDefect, ""); err == nil {
		t.Fatal("empty claim evidence accepted")
	}
}

func TestAttributionClaimChangesReportEncodingNotVectorIdentity(t *testing.T) {
	v, _ := base(t)
	id, err := (Runner[input, string, string, string]{Limits: DefaultLimits()}).Identity(v)
	if err != nil {
		t.Fatal(err)
	}
	r := finishReport(Report{Version: VersionV1, VectorID: id, Contrast: ContrastInvalid})
	before, err := r.Structured()
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := DeclareAttribution(r, AttributionImplementationDefect, "diagnostic ticket 17")
	if err != nil {
		t.Fatal(err)
	}
	after, err := claimed.Structured()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("claim absent from report encoding")
	}
	if claimed.VectorID != id || claimed.Classification.Attribution != AttributionUnresolved {
		t.Fatal("claim changed vector identity or authority")
	}
}

type panicAdapter struct{}

func (panicAdapter) Execute(context.Context, input) (Observation[string, string, string], error) {
	panic("adapter panic propagates")
}

func TestNonMatcherExecutablePanicPropagatesByPolicy(t *testing.T) {
	v, _ := base(t)
	snapshot, err := FreezeData(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindTrusted(snapshot, "adapter/panic/v1", panicAdapter{}, "classifier/access/v1", classifier)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("adapter panic was unexpectedly contained")
		}
	}()
	_ = bound.Run(context.Background())
}

func TestRunContrastOneCall(t *testing.T) {
	v, _ := base(t)
	r, err := RunContrast(context.Background(), ContrastRun[input, string, string, string]{
		Vector: v, AdapterKey: "adapter/access/v1", Adapter: &adapter{}, ClassifierKey: "classifier/access/v1", Classify: classifier,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Contrast != ContrastEstablished {
		t.Fatalf("%#v", r)
	}
}
