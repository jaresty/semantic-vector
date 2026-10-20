package vector

import (
	"context"
	"errors"
	"testing"
)

var errDenied = errors.New("denied")

type classifiedError struct {
	class string
	msg   string
	err   error
}

func (e classifiedError) Error() string        { return e.msg }
func (e classifiedError) Unwrap() error        { return e.err }
func (e classifiedError) FailureClass() string { return e.class }

func TestProvenanceDefaultsUnverifiedAndCannotSelfQualify(t *testing.T) {
	for _, independence := range []Independence{Reference, ImplementationDerived} {
		o := Oracle{Kind: "reference", Source: "declared", Revision: "1", Producer: "implementation", Method: "generated", Review: "Independence: reference", Independence: independence}
		if o.VerificationStatus() != VerificationUnverified || o.IndependentlyReviewed() {
			t.Fatalf("caller declaration self-qualified: %#v", o)
		}
	}
}

func TestDefaultUnverifiedReportNeverUpgradesAuthority(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = Verification{}
	r := Runner[input, string, string, string]{Adapter: &adapter{}, Classify: classifier, Limits: DefaultLimits()}.Run(context.Background(), v)
	if r.OracleQualified || r.Oracle.VerificationStatus() != VerificationUnverified || !has(r, CodeOracleUnqualified) {
		t.Fatalf("qualified=%t status=%s findings=%#v", r.OracleQualified, r.Oracle.VerificationStatus(), r.Findings)
	}
}

func TestIndependentReviewRequiresBoundedEvidence(t *testing.T) {
	o := Oracle{Kind: "reference", Source: "contract", Revision: "1", Producer: "author", Method: "manual", Independence: Reference, Verification: Verification{Status: VerificationIndependentlyReviewed}}
	if o.IndependentlyReviewed() {
		t.Fatal("review without bounded evidence qualified")
	}
	o.Verification.Evidence = []ReviewEvidence{{Reviewer: "reviewer", Scope: "two declared worlds", Source: "review/17", Revision: "abc"}}
	if !o.IndependentlyReviewed() {
		t.Fatal("complete independent review evidence was not represented")
	}
}

func TestFreezeCopiesInputsAndBindingHidesMutableSpec(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = reviewedEvidence()
	frozen, err := Freeze(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	original := frozen.Identity()
	v.Valid.Canonical[0] ^= 0xff
	v.ExpectedValid.Canonical.Output[0] ^= 0xff
	if frozen.Identity() != original {
		t.Fatal("post-freeze mutation changed identity")
	}
	bound, err := Bind(frozen, &adapter{}, classifier)
	if err != nil {
		t.Fatal(err)
	}
	r := bound.Run(context.Background())
	if r.Status != Pass {
		t.Fatalf("%#v", r.Findings)
	}
}

func TestTypedFailureClassExactAndErrorsIsMatcher(t *testing.T) {
	registry := NewFailureRegistry()
	if err := registry.Register("denied-v1", func(err error) bool { return errors.Is(err, errDenied) }); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("denied-v1", func(error) bool { return true }); !errors.Is(err, ErrDuplicateMatcherKey) {
		t.Fatalf("duplicate=%v", err)
	}

	actual := classifiedError{class: "permission-denied", msg: "wrapped: denied", err: errDenied}
	expect := FailureExpectation{Class: "permission-denied", MatcherKey: "denied-v1"}
	if !expect.Match(actual, registry) {
		t.Fatal("class and errors.Is-compatible matcher rejected wrapped identity")
	}
	wrongClass := expect
	wrongClass.Class = "other"
	if wrongClass.Match(actual, registry) {
		t.Fatal("stable failure class mismatch accepted")
	}
	exact := "denied"
	expect.ExactMessage = &exact
	if expect.Match(actual, registry) {
		t.Fatal("exact message was not independently enforced")
	}
}

func TestMatcherBindingExcludedFromCanonicalIdentity(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = reviewedEvidence()
	v.ExpectedViolating.FailureExpectation = FailureExpectation{Class: "denied", MatcherKey: "denied-v1"}
	a := NewFailureRegistry()
	_ = a.Register("denied-v1", func(error) bool { return true })
	b := NewFailureRegistry()
	_ = b.Register("denied-v1", func(error) bool { return false })
	fa, err := Freeze(v, a, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	fb, err := Freeze(v, b, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if fa.Identity() != fb.Identity() {
		t.Fatal("runtime function changed canonical identity")
	}
	missing := NewFailureRegistry()
	if _, err := Freeze(v, missing, DefaultLimits()); !errors.Is(err, ErrMissingMatcherKey) {
		t.Fatalf("missing=%v", err)
	}
}

func TestNoEffectsDistinctFromOmitted(t *testing.T) {
	if !NoEffects[string]().Known || NoEffects[string]().Values == nil {
		t.Fatal("NoEffects must assert an empty list")
	}
	v, _ := base(t)
	v.ExpectedViolating.Effects = Effects[string]{}
	if _, err := Freeze(v, nil, DefaultLimits()); err == nil {
		t.Fatal("omitted effects accepted")
	}
}

func TestReportClassifiesMismatchWithoutCausalAttribution(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = reviewedEvidence()
	f, err := Freeze(v, nil, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Bind(f, &adapter{badEffects: true}, classifier)
	r := b.Run(context.Background())
	if !r.Classification.ExpectationMismatch || r.Classification.ExecutionFailure || r.Classification.TestDefinitionDefect || r.Classification.Attribution != AttributionUnresolved {
		t.Fatalf("%#v", r.Classification)
	}

	v, _ = base(t)
	v.Oracle.Verification = reviewedEvidence()
	v.Perturbation.Difference.AfterDigest = v.Perturbation.Difference.BeforeDigest
	legacy := Runner[input, string, string, string]{Adapter: &adapter{}, Classify: classifier, Limits: DefaultLimits()}.Run(context.Background(), v)
	if !legacy.Classification.TestDefinitionDefect || legacy.Classification.Attribution != AttributionUnresolved {
		t.Fatalf("%#v", legacy.Classification)
	}
}

type rejectingEffectsCodec struct{ obsCodec }

func (*rejectingEffectsCodec) EncodeEffects([]string) ([]byte, error) {
	return nil, errors.New("reject effects")
}

func TestSimpleContrastDefaultEncodingFailsClosed(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = reviewedEvidence()
	v.ObservationCodec = &rejectingEffectsCodec{}
	v.ExpectedValid.Effects = Effects[string]{}
	v.ExpectedValid.Canonical.Effects = nil
	if _, err := FreezeContrast(ContrastSpec[input, string, string, string]{Vector: v}); err == nil {
		t.Fatal("default effects codec error discarded")
	}
}

func TestSimpleContrastDefaultsNoEffectsAndEvidenceCeiling(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = reviewedEvidence()
	v.ExpectedValid.Effects = Effects[string]{}
	v.ExpectedViolating.Effects = Effects[string]{}
	v.ExpectedValid.Canonical.Effects = nil
	v.ExpectedViolating.Canonical.Effects = nil
	v.EvidenceCeiling = EvidenceCeiling{}
	if _, err := FreezeContrast(ContrastSpec[input, string, string, string]{Vector: v}); err != nil {
		t.Fatal(err)
	}
}

func TestSimpleContrastHappyRefusalAndEqualWorlds(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Verification = reviewedEvidence()
	f, err := FreezeContrast(ContrastSpec[input, string, string, string]{Vector: v})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Bind(&adapter{}, classifier)
	if err != nil {
		t.Fatal(err)
	}
	if r := b.Run(context.Background()); r.Contrast != ContrastEstablished {
		t.Fatalf("%#v", r)
	}

	v2, _ := base(t)
	v2.Oracle.Verification = reviewedEvidence()
	v2.Perturbation.Apply = func(i input) (input, error) { return i, nil }
	v2.Perturbation.Difference.AfterDigest = v2.Perturbation.Difference.BeforeDigest
	if _, err := FreezeContrast(ContrastSpec[input, string, string, string]{Vector: v2}); err == nil {
		t.Fatal("equal worlds accepted")
	}
}

func reviewedEvidence() Verification {
	return Verification{Status: VerificationIndependentlyReviewed, Evidence: []ReviewEvidence{{Reviewer: "reviewer", Scope: "two declared worlds", Source: "review/17", Revision: "abc"}}}
}
