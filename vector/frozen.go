package vector

import (
	"bytes"
	"context"
	"errors"
	"sync"
)

var (
	ErrDuplicateMatcherKey = errors.New("duplicate failure matcher key")
	ErrMissingMatcherKey   = errors.New("missing failure matcher key")
)

type FailureMatcher func(error) bool

// ClassifiedFailure exposes a stable, message-independent failure class for
// in-memory matching. Implementations may also support errors.Is via Unwrap/Is.
type ClassifiedFailure interface {
	error
	FailureClass() string
}

type FailureRegistry struct {
	mu       sync.RWMutex
	matchers map[string]FailureMatcher
}

func NewFailureRegistry() *FailureRegistry {
	return &FailureRegistry{matchers: make(map[string]FailureMatcher)}
}

func (r *FailureRegistry) Register(key string, matcher FailureMatcher) error {
	if r == nil || key == "" || matcher == nil {
		return Issue{Code: CodeMissing, Surface: "failure.matcher", Detail: "matcher key and function required"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.matchers[key]; exists {
		return ErrDuplicateMatcherKey
	}
	r.matchers[key] = matcher
	return nil
}

func (r *FailureRegistry) Replace(key string, matcher FailureMatcher) error {
	if r == nil || key == "" || matcher == nil {
		return Issue{Code: CodeMissing, Surface: "failure.matcher", Detail: "existing matcher key and function required"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.matchers[key]; !exists {
		return ErrMissingMatcherKey
	}
	r.matchers[key] = matcher
	return nil
}

func (r *FailureRegistry) snapshot() map[string]FailureMatcher {
	out := make(map[string]FailureMatcher)
	if r == nil {
		return out
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for key, matcher := range r.matchers {
		out[key] = matcher
	}
	return out
}

func invokeMatcher(matcher FailureMatcher, actual error) (matched bool, err error) {
	if matcher == nil {
		return false, ErrMissingMatcherKey
	}
	defer func() {
		if recover() != nil {
			matched = false
			err = Issue{Code: CodeMatcherPanic, Surface: "failure.matcher", Detail: "matcher panicked"}
		}
	}()
	return matcher(actual), nil
}

func (e FailureExpectation) MatchResult(actual error, registry *FailureRegistry) (bool, error) {
	return e.match(actual, registry.snapshot())
}

// Match is retained for compatibility and fails closed on matcher panic. Use
// MatchResult when the typed execution failure must be reported.
func (e FailureExpectation) Match(actual error, registry *FailureRegistry) bool {
	matched, _ := e.MatchResult(actual, registry)
	return matched
}

func (e FailureExpectation) match(actual error, matchers map[string]FailureMatcher) (bool, error) {
	if e.Class == "" && e.ExactMessage == nil && e.MatcherKey == "" {
		return true, nil
	}
	if actual == nil {
		return false, nil
	}
	if e.Class != "" {
		var classified ClassifiedFailure
		if !errors.As(actual, &classified) || classified.FailureClass() != e.Class {
			return false, nil
		}
	}
	if e.ExactMessage != nil && actual.Error() != *e.ExactMessage {
		return false, nil
	}
	if e.MatcherKey != "" {
		matcher, ok := matchers[e.MatcherKey]
		if !ok {
			return false, ErrMissingMatcherKey
		}
		matched, err := invokeMatcher(matcher, actual)
		if err != nil || !matched {
			return false, err
		}
	}
	return true, nil
}

// FrozenDataSnapshot owns immutable copies of canonical data while retaining
// trusted executable dependencies by declared custody key. It does not freeze
// closure state or make those dependencies concurrency-safe.
type FrozenDataSnapshot[I, O, F, E any] struct {
	vector   Vector[I, O, F, E]
	prepared prepared
	matchers map[string]FailureMatcher
	limits   Limits
}

// FrozenSpec is retained as a source-compatible alias.
// Deprecated: use FrozenDataSnapshot.
type FrozenSpec[I, O, F, E any] = FrozenDataSnapshot[I, O, F, E]

func (f FrozenDataSnapshot[I, O, F, E]) Identity() string { return f.prepared.id }
func (f FrozenDataSnapshot[I, O, F, E]) ExecutableCustody() ExecutableCustody {
	return f.vector.Executables
}
func (f FrozenDataSnapshot[I, O, F, E]) matcher(key string) FailureMatcher { return f.matchers[key] }

func cloneVector[I, O, F, E any](v Vector[I, O, F, E]) Vector[I, O, F, E] {
	v.Valid.Canonical = append([]byte(nil), v.Valid.Canonical...)
	if v.Oracle.Verification.Status != VerificationIndependentlyReviewed {
		v.Oracle.Verification.Status = VerificationUnverified
	}
	v.ExpectedValid.Canonical = cloneExpectation(v.ExpectedValid.Canonical)
	v.ExpectedViolating.Canonical = cloneExpectation(v.ExpectedViolating.Canonical)
	v.ExpectedValid.Effects.Values = append([]E(nil), v.ExpectedValid.Effects.Values...)
	v.ExpectedViolating.Effects.Values = append([]E(nil), v.ExpectedViolating.Effects.Values...)
	v.EvidenceCeiling.CannotEstablish = append([]string(nil), v.EvidenceCeiling.CannotEstablish...)
	v.Oracle.Verification.Evidence = append([]ReviewEvidence(nil), v.Oracle.Verification.Evidence...)
	if v.ExpectedValid.FailureExpectation.ExactMessage != nil {
		s := *v.ExpectedValid.FailureExpectation.ExactMessage
		v.ExpectedValid.FailureExpectation.ExactMessage = &s
	}
	if v.ExpectedViolating.FailureExpectation.ExactMessage != nil {
		s := *v.ExpectedViolating.FailureExpectation.ExactMessage
		v.ExpectedViolating.FailureExpectation.ExactMessage = &s
	}
	return v
}

func FreezeData[I, O, F, E any](v Vector[I, O, F, E], registry *FailureRegistry, limits Limits) (FrozenDataSnapshot[I, O, F, E], error) {
	matchers := registry.snapshot()
	for _, x := range []FailureExpectation{v.ExpectedValid.FailureExpectation, v.ExpectedViolating.FailureExpectation} {
		if x.MatcherKey != "" {
			if _, ok := matchers[x.MatcherKey]; !ok {
				return FrozenDataSnapshot[I, O, F, E]{}, errors.Join(ErrMissingMatcherKey, Issue{Code: CodeMissing, Surface: "failure.matcher_key", Detail: x.MatcherKey})
			}
		}
	}
	copy := cloneVector(v)
	r := Runner[I, O, F, E]{Limits: limits}
	p, err := r.prepare(copy)
	if err != nil {
		return FrozenDataSnapshot[I, O, F, E]{}, err
	}
	copy.ID = p.id
	return FrozenDataSnapshot[I, O, F, E]{vector: copy, prepared: p, matchers: matchers, limits: r.limits()}, nil
}

// Freeze is retained as a deprecated alias for FreezeData.
func Freeze[I, O, F, E any](v Vector[I, O, F, E], registry *FailureRegistry, limits Limits) (FrozenDataSnapshot[I, O, F, E], error) {
	return FreezeData(v, registry, limits)
}

type BoundRunner[I, O, F, E any] struct {
	frozen   FrozenDataSnapshot[I, O, F, E]
	adapter  Adapter[I, O, F, E]
	classify Classifier[O, F, E]
	custody  BindingCustody
}

func BindTrusted[I, O, F, E any](f FrozenDataSnapshot[I, O, F, E], adapterKey string, adapter Adapter[I, O, F, E], classifierKey string, classify Classifier[O, F, E]) (BoundRunner[I, O, F, E], error) {
	if adapter == nil || classify == nil || adapterKey == "" || classifierKey == "" {
		return BoundRunner[I, O, F, E]{}, Issue{Code: CodeMissing, Surface: "binding", Detail: "stable adapter/classifier keys and implementations required"}
	}
	return BoundRunner[I, O, F, E]{frozen: f, adapter: adapter, classify: classify, custody: BindingCustody{AdapterKey: adapterKey, ClassifierKey: classifierKey}}, nil
}

// Bind is retained for compatibility. Its report marks the binding as
// compatibility-only because no stable adapter/classifier identity was supplied.
func Bind[I, O, F, E any](f FrozenDataSnapshot[I, O, F, E], adapter Adapter[I, O, F, E], classify Classifier[O, F, E]) (BoundRunner[I, O, F, E], error) {
	if adapter == nil || classify == nil {
		return BoundRunner[I, O, F, E]{}, Issue{Code: CodeMissing, Surface: "binding", Detail: "adapter and classifier required"}
	}
	return BoundRunner[I, O, F, E]{frozen: f, adapter: adapter, classify: classify, custody: BindingCustody{AdapterKey: "unkeyed", ClassifierKey: "unkeyed", Compatibility: true}}, nil
}

func (r BoundRunner[I, O, F, E]) execute(ctx context.Context, world string, canonical []byte, expected Expectation[O, F, E], encodedExpected EncodedObservation) (WorldResult, error) {
	v := r.frozen.vector
	value, err := decodeCopy(v.Codec, canonical)
	if err != nil {
		return WorldResult{}, Issue{Code: CodeUnsupported, World: world, Surface: "codec.decode", Detail: "execution decode failed"}
	}
	roundTrip, err := stableEncode("world."+world+".execution", value, v.Codec.Encode, r.frozen.limits.MaxCanonicalBytes)
	if err != nil {
		return WorldResult{}, err
	}
	if !bytes.Equal(roundTrip, canonical) {
		return WorldResult{}, Issue{Code: CodeInvalid, World: world, Surface: "codec", Detail: "execution round-trip changed canonical bytes"}
	}
	observation, err := r.adapter.Execute(ctx, value)
	if err != nil {
		return WorldResult{}, Issue{Code: CodeAdapter, World: world, Surface: "adapter", Detail: "adapter failed"}
	}
	classification, err := r.classify(observation)
	if err != nil {
		return WorldResult{}, Issue{Code: CodeClassifier, World: world, Surface: "classifier", Detail: "classifier failed"}
	}
	encoded, err := encodeObservation(observation, v.ObservationCodec, r.frozen.limits)
	if err != nil {
		return WorldResult{}, err
	}
	typedFailure := expected.FailureExpectation.Class != "" || expected.FailureExpectation.ExactMessage != nil || expected.FailureExpectation.MatcherKey != ""
	if typedFailure && expected.FailureExpectation.ExactMessage == nil {
		// A declared class/matcher intentionally avoids coupling to incidental text.
		encodedExpected.Failure = encoded.Failure
	}
	result := compare(world, encoded, encodedExpected, classification, expected.Classification)
	if typedFailure {
		actual, ok := any(observation.Failure).(error)
		if !ok {
			result.Matched = false
			result.Findings = append(result.Findings, Finding{Code: CodeMismatch, World: world, Surface: "failure.typed", Detail: "typed failure expectation differs"})
		} else {
			matched, matchErr := expected.FailureExpectation.match(actual, r.frozen.matchers)
			if matchErr != nil {
				return WorldResult{}, matchErr
			}
			if !matched {
				result.Matched = false
				result.Findings = append(result.Findings, Finding{Code: CodeMismatch, World: world, Surface: "failure.typed", Detail: "typed failure expectation differs"})
			}
		}
	}
	return result, nil
}

func (r BoundRunner[I, O, F, E]) Run(ctx context.Context) Report {
	f := r.frozen
	v, p, l := f.vector, f.prepared, f.limits
	rep := newReport(v, l)
	rep.VectorID, rep.ReproductionID, rep.Binding = p.id, p.id, r.custody
	eev, err := expectedEncoded(p.validExpected, l)
	if err != nil {
		rep.Findings = append(rep.Findings, issueFrom(err, CodeOverflow, "valid", "report"))
		return finishReport(rep)
	}
	eex, err := expectedEncoded(p.violatingExpected, l)
	if err != nil {
		rep.Findings = append(rep.Findings, issueFrom(err, CodeOverflow, "violating", "report"))
		return finishReport(rep)
	}
	rep.Valid, err = r.execute(ctx, "valid", p.validBytes, v.ExpectedValid, eev)
	if err != nil {
		rep.Findings = append(rep.Findings, issueFrom(err, CodeInvalid, "valid", "execution"))
		return finishReport(rep)
	}
	rep.Violating, err = r.execute(ctx, "violating", p.violatingBytes, v.ExpectedViolating, eex)
	if err != nil {
		rep.Findings = append(rep.Findings, issueFrom(err, CodeInvalid, "violating", "execution"))
		return finishReport(rep)
	}
	rep.Findings = append(rep.Findings, rep.Valid.Findings...)
	rep.Findings = append(rep.Findings, rep.Violating.Findings...)
	cv, cx := rep.Valid.Classification, rep.Violating.Classification
	if cv == cx {
		rep.Contrast = ContrastAbsent
		rep.Findings = append(rep.Findings, Finding{Code: CodeNondiscriminating, Surface: "classification", Detail: "observed classifications are identical"})
	} else if cv == v.ExpectedValid.Classification && cx == v.ExpectedViolating.Classification {
		rep.Contrast = ContrastEstablished
	} else {
		rep.Contrast = ContrastInvalid
	}
	return finishReport(rep)
}

type ContrastSpec[I, O, F, E any] struct{ Vector Vector[I, O, F, E] }
type FrozenContrast[I, O, F, E any] struct {
	spec FrozenDataSnapshot[I, O, F, E]
}

func applyContrastDefaults[I, O, F, E any](c ContrastSpec[I, O, F, E]) (ContrastSpec[I, O, F, E], error) {
	if c.Vector.ObservationCodec == nil {
		return c, Issue{Code: CodeMissing, Surface: "codec", Detail: "observation codec required"}
	}
	if !c.Vector.ExpectedValid.Effects.Known {
		encoded, err := c.Vector.ObservationCodec.EncodeEffects([]E{})
		if err != nil {
			return c, Issue{Code: CodeUnsupported, World: "valid", Surface: "expectation.effects", Detail: "codec rejected default no-effects"}
		}
		c.Vector.ExpectedValid.Effects = NoEffects[E]()
		c.Vector.ExpectedValid.Canonical.Effects = append([]byte(nil), encoded...)
	}
	if !c.Vector.ExpectedViolating.Effects.Known {
		encoded, err := c.Vector.ObservationCodec.EncodeEffects([]E{})
		if err != nil {
			return c, Issue{Code: CodeUnsupported, World: "violating", Surface: "expectation.effects", Detail: "codec rejected default no-effects"}
		}
		c.Vector.ExpectedViolating.Effects = NoEffects[E]()
		c.Vector.ExpectedViolating.Canonical.Effects = append([]byte(nil), encoded...)
	}
	if c.Vector.EvidenceCeiling.Version == "" && len(c.Vector.EvidenceCeiling.CannotEstablish) == 0 {
		c.Vector.EvidenceCeiling = EvidenceCeiling{Version: "contrast/v1", CannotEstablish: []string{"domain truth", "semantic completeness", "author independence"}}
	}
	return c, nil
}

func FreezeContrast[I, O, F, E any](c ContrastSpec[I, O, F, E]) (FrozenContrast[I, O, F, E], error) {
	withDefaults, err := applyContrastDefaults(c)
	if err != nil {
		return FrozenContrast[I, O, F, E]{}, err
	}
	f, err := FreezeData(withDefaults.Vector, nil, DefaultLimits())
	return FrozenContrast[I, O, F, E]{spec: f}, err
}

func CheckContrast[I, O, F, E any](c ContrastSpec[I, O, F, E]) (FrozenContrast[I, O, F, E], error) {
	return FreezeContrast(c)
}

func (f FrozenContrast[I, O, F, E]) Bind(adapter Adapter[I, O, F, E], classify Classifier[O, F, E]) (BoundRunner[I, O, F, E], error) {
	return Bind(f.spec, adapter, classify)
}

type ContrastRun[I, O, F, E any] struct {
	Vector        Vector[I, O, F, E]
	Registry      *FailureRegistry
	Limits        Limits
	AdapterKey    string
	Adapter       Adapter[I, O, F, E]
	ClassifierKey string
	Classify      Classifier[O, F, E]
}

// RunContrast applies safer lightweight defaults and performs FreezeData,
// BindTrusted, and Run in one call. It still requires an explicit Vector.
func RunContrast[I, O, F, E any](ctx context.Context, run ContrastRun[I, O, F, E]) (Report, error) {
	withDefaults, err := applyContrastDefaults(ContrastSpec[I, O, F, E]{Vector: run.Vector})
	if err != nil {
		return Report{}, err
	}
	limits := run.Limits
	if limits.MaxCanonicalBytes <= 0 || limits.MaxMismatchBytes <= 0 || limits.MaxRenderBytes <= 0 {
		limits = DefaultLimits()
	}
	frozen, err := FreezeData(withDefaults.Vector, run.Registry, limits)
	if err != nil {
		return Report{}, err
	}
	bound, err := BindTrusted(frozen, run.AdapterKey, run.Adapter, run.ClassifierKey, run.Classify)
	if err != nil {
		return Report{}, err
	}
	return bound.Run(ctx), nil
}
