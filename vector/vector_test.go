package vector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Encode(v T) ([]byte, error) { return json.Marshal(v) }
func (jsonCodec[T]) Decode(b []byte) (T, error) { var v T; err := json.Unmarshal(b, &v); return v, err }

type obsCodec struct {
	unstable  bool
	n         int
	oversized bool
}

func (c *obsCodec) enc(v any) ([]byte, error) {
	c.n++
	if c.unstable {
		return []byte{byte(c.n)}, nil
	}
	if c.oversized {
		return make([]byte, 1024), nil
	}
	return json.Marshal(v)
}
func (c *obsCodec) EncodeOutput(v string) ([]byte, error)    { return c.enc(v) }
func (c *obsCodec) EncodeFailure(v string) ([]byte, error)   { return c.enc(v) }
func (c *obsCodec) EncodeEffects(v []string) ([]byte, error) { return c.enc(v) }

type input struct {
	Allowed bool     `json:"allowed"`
	Tags    []string `json:"tags"`
}
type adapter struct {
	calls                                   int
	badEffects, incidental, oversizedOutput bool
	err                                     error
}

func (a *adapter) Execute(_ context.Context, in input) (Observation[string, string, string], error) {
	a.calls++
	if a.err != nil {
		return Observation[string, string, string]{}, a.err
	}
	if in.Allowed {
		return Observation[string, string, string]{Output: "ok", Effects: []string{"write"}}, nil
	}
	out := ""
	if a.incidental {
		out = "different"
	}
	if a.oversizedOutput {
		out = strings.Repeat("x", 1024)
	}
	effects := []string{}
	if a.badEffects {
		effects = []string{"write"}
	}
	return Observation[string, string, string]{Output: out, Failure: "denied", Effects: effects}, nil
}
func classifier(o Observation[string, string, string]) (string, error) {
	if o.Failure == "denied" {
		return "denied", nil
	}
	return "allowed", nil
}
func constantClassifier(Observation[string, string, string]) (string, error) { return "same", nil }
func canon(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func expectation(t *testing.T, c *obsCodec, o, f, class string, effects []string) Expectation[string, string, string] {
	t.Helper()
	ob, _ := c.EncodeOutput(o)
	fb, _ := c.EncodeFailure(f)
	eb, _ := c.EncodeEffects(normalized(effects))
	return Expectation[string, string, string]{o, f, Effects[string]{true, effects}, class, CanonicalExpectation{ob, fb, eb}}
}
func base(t *testing.T) (Vector[input, string, string, string], *obsCodec) {
	t.Helper()
	c := &obsCodec{}
	valid := input{true, []string{"original"}}
	bad := input{false, []string{"original"}}
	vb, bb := canon(t, valid), canon(t, bad)
	v := Vector[input, string, string, string]{Version: Version, Property: Property{"allow", "allowed requests succeed", "contract", "1"}, Oracle: Oracle{"reference", "contract", "1", "reviewer", "manual", "reviewed", Independent}, Codec: jsonCodec[input]{}, ObservationCodec: c, Valid: World[input]{vb}, Perturbation: Perturbation[input]{"deny", WholeWorldDifference{digest(vb), digest(bb)}, func(i input) (input, error) { i.Allowed = false; return i, nil }}, ExpectedValid: expectation(t, c, "ok", "", "allowed", []string{"write"}), ExpectedViolating: expectation(t, c, "", "denied", "denied", nil), EvidenceCeiling: EvidenceCeiling{"v0", []string{"semantic completeness"}}}
	return v, c
}
func run(t *testing.T, v Vector[input, string, string, string], a *adapter, class Classifier[string, string, string]) Report {
	t.Helper()
	return Runner[input, string, string, string]{a, class, DefaultLimits()}.Run(context.Background(), v)
}
func has(r Report, c Code) bool {
	for _, f := range r.Findings {
		if f.Code == c {
			return true
		}
	}
	return false
}
func TestPassAndWholeWorldIdentity(t *testing.T) {
	v, _ := base(t)
	a := &adapter{}
	r := run(t, v, a, classifier)
	if r.Status != Pass || r.Contrast != ContrastEstablished || a.calls != 2 {
		t.Fatalf("%s %s calls=%d findings=%#v", r.Status, r.Contrast, a.calls, r.Findings)
	}
	id, e := Runner[input, string, string, string]{Limits: DefaultLimits()}.Identity(v)
	if e != nil || id != r.VectorID {
		t.Fatalf("id=%s report=%s err=%v", id, r.VectorID, e)
	}
}
func TestArbitraryDifferenceMetadataRejected(t *testing.T) {
	v, _ := base(t)
	v.Perturbation.Difference.AfterDigest = "sha256:invented"
	a := &adapter{}
	r := run(t, v, a, classifier)
	if !has(r, CodeInvalid) || a.calls != 0 {
		t.Fatalf("findings=%#v calls=%d", r.Findings, a.calls)
	}
}
func TestMutatorAliasingCannotCorruptValidWorld(t *testing.T) {
	v, _ := base(t)
	v.Perturbation.Apply = func(i input) (input, error) { i.Allowed = false; i.Tags[0] = "mutated"; return i, nil }
	bad := input{false, []string{"mutated"}}
	v.Perturbation.Difference.AfterDigest = digest(canon(t, bad))
	a := &adapter{}
	r := run(t, v, a, classifier)
	if r.Status != Pass {
		t.Fatalf("%#v", r.Findings)
	}
	if r.Valid.Observation.Output == "" {
		t.Fatal("valid copy was corrupted")
	}
}
func TestIncidentalDifferenceDoesNotEstablishContrast(t *testing.T) {
	v, _ := base(t)
	v.ExpectedViolating.Output = "different"
	v.ExpectedViolating.Canonical.Output = canon(t, "different")
	a := &adapter{incidental: true}
	r := run(t, v, a, constantClassifier)
	if r.Contrast != ContrastAbsent || !has(r, CodeNondiscriminating) {
		t.Fatalf("contrast=%s findings=%#v", r.Contrast, r.Findings)
	}
}
func TestOracleReviewRequired(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Review = ""
	a := &adapter{}
	r := run(t, v, a, classifier)
	if !has(r, CodeMissing) || a.calls != 0 {
		t.Fatalf("%#v calls=%d", r.Findings, a.calls)
	}
}
func TestOracleFindingNotMaskedByMismatch(t *testing.T) {
	v, _ := base(t)
	v.Oracle.Independence = Unknown
	a := &adapter{badEffects: true}
	r := run(t, v, a, classifier)
	if !has(r, CodeOracleUnqualified) || !has(r, CodeMismatch) {
		t.Fatalf("%#v", r.Findings)
	}
}
func TestTypedApplyAdapterAndNilErrors(t *testing.T) {
	t.Run("apply", func(t *testing.T) {
		v, _ := base(t)
		v.Perturbation.Apply = func(input) (input, error) { return input{}, errors.New("secret") }
		r := run(t, v, &adapter{}, classifier)
		if !has(r, CodeApply) {
			t.Fatalf("%#v", r.Findings)
		}
	})
	t.Run("adapter", func(t *testing.T) {
		v, _ := base(t)
		r := run(t, v, &adapter{err: errors.New("secret")}, classifier)
		if !has(r, CodeAdapter) {
			t.Fatalf("%#v", r.Findings)
		}
	})
	t.Run("nil", func(t *testing.T) {
		v, _ := base(t)
		r := Runner[input, string, string, string]{Classify: classifier, Limits: DefaultLimits()}.Run(context.Background(), v)
		if !has(r, CodeMissing) {
			t.Fatalf("%#v", r.Findings)
		}
	})
}
func TestValidationFailureNeverCallsAdapter(t *testing.T) {
	v, _ := base(t)
	v.Property.ID = ""
	a := &adapter{}
	r := run(t, v, a, classifier)
	if a.calls != 0 || !has(r, CodeMissing) {
		t.Fatalf("calls=%d findings=%#v", a.calls, r.Findings)
	}
}
func TestNilAndEmptyKnownZeroHaveSameIdentity(t *testing.T) {
	a, _ := base(t)
	b, _ := base(t)
	b.ExpectedViolating.Effects.Values = []string{}
	b.ExpectedViolating.Canonical.Effects = canon(t, []string{})
	runner := Runner[input, string, string, string]{Limits: DefaultLimits()}
	ai, e := runner.Identity(a)
	if e != nil {
		t.Fatal(e)
	}
	bi, e := runner.Identity(b)
	if e != nil {
		t.Fatal(e)
	}
	if ai != bi {
		t.Fatalf("%s != %s", ai, bi)
	}
}
func TestPerturbationIDAffectsIdentity(t *testing.T) {
	a, _ := base(t)
	b, _ := base(t)
	b.Perturbation.ID = "other"
	runner := Runner[input, string, string, string]{Limits: DefaultLimits()}
	ai, _ := runner.Identity(a)
	bi, _ := runner.Identity(b)
	if ai == bi {
		t.Fatal("perturbation id omitted from identity")
	}
}
func TestUnstableCodecRejected(t *testing.T) {
	v, _ := base(t)
	u := &obsCodec{unstable: true}
	v.ObservationCodec = u
	v.ExpectedValid.Canonical = CanonicalExpectation{[]byte{1}, []byte{3}, []byte{5}}
	r := run(t, v, &adapter{}, classifier)
	if !has(r, CodeUnstableEncoding) {
		t.Fatalf("%#v", r.Findings)
	}
}
func TestOversizedObservationAndRenderAreTyped(t *testing.T) {
	v, _ := base(t)
	r := Runner[input, string, string, string]{&adapter{oversizedOutput: true}, classifier, Limits{2048, 16, 4096}}.Run(context.Background(), v)
	if !has(r, CodeOverflow) {
		t.Fatalf("%#v", r.Findings)
	}
	v, _ = base(t)
	r = run(t, v, &adapter{}, classifier)
	r.limits.MaxRenderBytes = 8
	if _, e := r.Structured(); e == nil {
		t.Fatal("structured overflow accepted")
	}
	if _, e := r.Human(); e == nil {
		t.Fatal("human overflow accepted")
	}
}

type hostile string

func (hostile) String() string               { panic("formatter invoked") }
func (hostile) MarshalJSON() ([]byte, error) { panic("marshaler invoked") }

type hostileCodec struct{}

func (hostileCodec) EncodeOutput(v hostile) ([]byte, error)   { return []byte(v), nil }
func (hostileCodec) EncodeFailure(v string) ([]byte, error)   { return []byte(v), nil }
func (hostileCodec) EncodeEffects(v []string) ([]byte, error) { return json.Marshal(normalized(v)) }

type hostileAdapter struct{}

func (hostileAdapter) Execute(_ context.Context, in input) (Observation[hostile, string, string], error) {
	if in.Allowed {
		return Observation[hostile, string, string]{Output: hostile("ok"), Effects: []string{}}, nil
	}
	return Observation[hostile, string, string]{Output: hostile("bad"), Failure: "denied", Effects: []string{}}, nil
}
func TestReportRenderingDoesNotInvokeDomainFormatters(t *testing.T) {
	valid := input{Allowed: true}
	bad := input{Allowed: false}
	vb, bb := canon(t, valid), canon(t, bad)
	zero := canon(t, []string{})
	v := Vector[input, hostile, string, string]{Version: Version, Property: Property{"p", "statement", "source", ""}, Oracle: Oracle{"reference", "source", "1", "reviewer", "manual", "reviewed", Reference}, Codec: jsonCodec[input]{}, ObservationCodec: hostileCodec{}, Valid: World[input]{vb}, Perturbation: Perturbation[input]{"change", WholeWorldDifference{digest(vb), digest(bb)}, func(i input) (input, error) { i.Allowed = false; return i, nil }}, ExpectedValid: Expectation[hostile, string, string]{hostile("ok"), "", Effects[string]{true, []string{}}, "allowed", CanonicalExpectation{[]byte("ok"), []byte{}, zero}}, ExpectedViolating: Expectation[hostile, string, string]{hostile("bad"), "denied", Effects[string]{true, []string{}}, "denied", CanonicalExpectation{[]byte("bad"), []byte("denied"), zero}}, EvidenceCeiling: EvidenceCeiling{"v0", []string{"authority"}}}
	classify := func(o Observation[hostile, string, string]) (string, error) {
		if o.Failure != "" {
			return "denied", nil
		}
		return "allowed", nil
	}
	r := Runner[input, hostile, string, string]{hostileAdapter{}, classify, DefaultLimits()}.Run(context.Background(), v)
	if r.Status != Pass {
		t.Fatalf("%#v", r.Findings)
	}
	if _, e := r.Structured(); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Human(); e != nil {
		t.Fatal(e)
	}
}

type cachedPointerCodec struct{ cached *input }

func (c *cachedPointerCodec) Encode(v *input) ([]byte, error) { return json.Marshal(v) }
func (c *cachedPointerCodec) Decode(b []byte) (*input, error) {
	if c.cached == nil {
		c.cached = &input{}
	}
	if err := json.Unmarshal(b, c.cached); err != nil {
		return nil, err
	}
	return c.cached, nil
}

type pointerAdapter struct {
	calls int
	seen  []bool
}

func (a *pointerAdapter) Execute(_ context.Context, v *input) (Observation[string, string, string], error) {
	a.calls++
	a.seen = append(a.seen, v.Allowed)
	if v.Allowed {
		return Observation[string, string, string]{Output: "ok", Effects: []string{"write"}}, nil
	}
	return Observation[string, string, string]{Failure: "denied", Effects: []string{}}, nil
}
func TestCachedPointerCodecCannotCorruptValidExecution(t *testing.T) {
	codec := &cachedPointerCodec{}
	observations := &obsCodec{}
	valid := &input{Allowed: true}
	bad := &input{Allowed: false}
	vb, bb := canon(t, valid), canon(t, bad)
	v := Vector[*input, string, string, string]{Version: Version, Property: Property{"p", "statement", "source", ""}, Oracle: Oracle{"reference", "source", "1", "reviewer", "manual", "reviewed", Reference}, Codec: codec, ObservationCodec: observations, Valid: World[*input]{vb}, Perturbation: Perturbation[*input]{"deny", WholeWorldDifference{digest(vb), digest(bb)}, func(i *input) (*input, error) { i.Allowed = false; return i, nil }}, ExpectedValid: expectation(t, observations, "ok", "", "allowed", []string{"write"}), ExpectedViolating: expectation(t, observations, "", "denied", "denied", nil), EvidenceCeiling: EvidenceCeiling{"v0", []string{"authority"}}}
	classify := func(o Observation[string, string, string]) (string, error) {
		if o.Failure != "" {
			return "denied", nil
		}
		return "allowed", nil
	}
	a := &pointerAdapter{}
	r := Runner[*input, string, string, string]{a, classify, DefaultLimits()}.Run(context.Background(), v)
	if r.Status != Pass || r.Contrast != ContrastEstablished || a.calls != 2 {
		t.Fatalf("status=%s contrast=%s calls=%d findings=%#v", r.Status, r.Contrast, a.calls, r.Findings)
	}
	if len(a.seen) != 2 || !a.seen[0] || a.seen[1] {
		t.Fatalf("execution order observed %#v", a.seen)
	}
}

type failingBeforeEffectsAdapter struct{ calls, effects int }

func (a *failingBeforeEffectsAdapter) Execute(context.Context, input) (Observation[string, string, string], error) {
	a.calls++
	return Observation[string, string, string]{}, errors.New("stop before effects")
}
func TestValidFailureStopsBeforeViolatingDecodeAndEffects(t *testing.T) {
	v, _ := base(t)
	a := &failingBeforeEffectsAdapter{}
	r := Runner[input, string, string, string]{a, classifier, DefaultLimits()}.Run(context.Background(), v)
	if !has(r, CodeAdapter) || a.calls != 1 || a.effects != 0 {
		t.Fatalf("calls=%d effects=%d findings=%#v", a.calls, a.effects, r.Findings)
	}
}

type phaseLimitedObservationCodec struct{ calls, max int }

func (c *phaseLimitedObservationCodec) encode(v any) ([]byte, error) {
	c.calls++
	if c.calls > c.max {
		return nil, errors.New("unexpected expectation recode")
	}
	return json.Marshal(v)
}
func (c *phaseLimitedObservationCodec) EncodeOutput(v string) ([]byte, error)  { return c.encode(v) }
func (c *phaseLimitedObservationCodec) EncodeFailure(v string) ([]byte, error) { return c.encode(v) }
func (c *phaseLimitedObservationCodec) EncodeEffects(v []string) ([]byte, error) {
	return c.encode(normalized(v))
}
func TestPreparedExpectationsAreNotRecodedByRun(t *testing.T) {
	v, _ := base(t)
	codec := &phaseLimitedObservationCodec{max: 24}
	v.ObservationCodec = codec
	a := &adapter{}
	r := run(t, v, a, classifier)
	if r.Status != Pass || codec.calls != 24 || a.calls != 2 {
		t.Fatalf("status=%s codec-calls=%d adapter-calls=%d findings=%#v", r.Status, codec.calls, a.calls, r.Findings)
	}
}
func TestPreparationCodecFailureIsFailClosed(t *testing.T) {
	v, _ := base(t)
	codec := &phaseLimitedObservationCodec{max: 11}
	v.ObservationCodec = codec
	a := &adapter{}
	r := run(t, v, a, classifier)
	if r.Status == Pass || a.calls != 0 || !has(r, CodeUnsupported) {
		t.Fatalf("status=%s codec-calls=%d adapter-calls=%d findings=%#v", r.Status, codec.calls, a.calls, r.Findings)
	}
}

type mutatingDecodeCodec struct{ calls int }

func (c *mutatingDecodeCodec) Encode(v input) ([]byte, error) { return json.Marshal(v) }
func (c *mutatingDecodeCodec) Decode(b []byte) (input, error) {
	c.calls++
	clean := append([]byte(nil), b...)
	for i := range b {
		b[i] ^= 0xff
	}
	var v input
	err := json.Unmarshal(clean, &v)
	return v, err
}
func TestDecodeAlwaysReceivesDefensiveCanonicalCopy(t *testing.T) {
	v, _ := base(t)
	codec := &mutatingDecodeCodec{}
	v.Codec = codec
	original := append([]byte(nil), v.Valid.Canonical...)
	runner := Runner[input, string, string, string]{Limits: DefaultLimits()}
	id1, err := runner.Identity(v)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := runner.Identity(v)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("identity changed: %s != %s", id1, id2)
	}
	if string(v.Valid.Canonical) != string(original) {
		t.Fatal("identity mutated vector canonical custody")
	}
	a := &adapter{}
	r := Runner[input, string, string, string]{a, classifier, DefaultLimits()}.Run(context.Background(), v)
	if r.Status != Pass || r.Contrast != ContrastEstablished || a.calls != 2 {
		t.Fatalf("status=%s contrast=%s calls=%d findings=%#v", r.Status, r.Contrast, a.calls, r.Findings)
	}
	if string(v.Valid.Canonical) != string(original) {
		t.Fatal("execution mutated vector canonical custody")
	}
}
func TestDecodeCopyBlocksDirectSliceEscapeMutation(t *testing.T) {
	codec := &mutatingDecodeCodec{}
	source := canon(t, input{Allowed: true})
	original := append([]byte(nil), source...)
	escaped := append([]byte(nil), source...)
	if _, err := codec.Decode(escaped); err != nil {
		t.Fatal(err)
	}
	if string(escaped) == string(original) {
		t.Fatal("hostile codec did not exercise mutation")
	}
	if _, err := decodeCopy[input](codec, source); err != nil {
		t.Fatal(err)
	}
	if string(source) != string(original) {
		t.Fatal("decodeCopy exposed source bytes")
	}
}

func TestReportsDeterministic(t *testing.T) {
	v, _ := base(t)
	r := run(t, v, &adapter{}, classifier)
	a, e := r.Structured()
	if e != nil {
		t.Fatal(e)
	}
	b, _ := r.Structured()
	if string(a) != string(b) {
		t.Fatal("structured changed")
	}
	h, e := r.Human()
	if e != nil {
		t.Fatal(e)
	}
	h2, _ := r.Human()
	if h != h2 || !strings.Contains(h, "semantic completeness") {
		t.Fatal("human report unstable or unbounded")
	}
}
