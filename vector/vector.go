// Package vector provides small, generic, in-memory semantic contrast vectors.
// Callers supply deterministic codecs: the package does not infer domain
// structure, canonicalize arbitrary values, or claim semantic completeness.
package vector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const Version = "semantic-vector/v0"

var ErrValidation = errors.New("semantic vector validation failed")

type Code string

const (
	CodeMissing           Code = "MISSING"
	CodeInvalid           Code = "INVALID"
	CodeUnsupported       Code = "UNSUPPORTED"
	CodeUnstableEncoding  Code = "UNSTABLE_ENCODING"
	CodeOverflow          Code = "OVERFLOW"
	CodeApply             Code = "APPLY_ERROR"
	CodeAdapter           Code = "ADAPTER_ERROR"
	CodeClassifier        Code = "CLASSIFIER_ERROR"
	CodeMismatch          Code = "MISMATCH"
	CodeOracleUnqualified Code = "ORACLE_UNQUALIFIED"
	CodeNondiscriminating Code = "NONDISCRIMINATING"
)

type Issue struct {
	Code    Code   `json:"code"`
	World   string `json:"world,omitempty"`
	Surface string `json:"surface,omitempty"`
	Detail  string `json:"detail"`
}

func (i Issue) Error() string {
	return fmt.Sprintf("%s world=%q surface=%q: %s", i.Code, i.World, i.Surface, i.Detail)
}
func (i Issue) Unwrap() error { return ErrValidation }

type Property struct{ ID, Statement, Source, Version string }

type Independence string

const (
	Independent           Independence = "independent"
	Diverse               Independence = "diverse"
	Reference             Independence = "reference"
	GeneratorDependent    Independence = "generator-dependent"
	ImplementationDerived Independence = "implementation-derived"
	Unknown               Independence = "unknown"
)

type Oracle struct {
	Kind, Source, Revision, Producer, Method, Review string
	Independence                                     Independence
}

func (o Oracle) Qualified() bool {
	return o.Independence == Independent || o.Independence == Diverse || o.Independence == Reference
}

type EvidenceCeiling struct {
	Version         string
	CannotEstablish []string
}
type Codec[T any] interface {
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}
type ObservationCodec[O, F, E any] interface {
	EncodeOutput(O) ([]byte, error)
	EncodeFailure(F) ([]byte, error)
	EncodeEffects([]E) ([]byte, error)
}
type World[T any] struct{ Canonical []byte }
type Effects[E any] struct {
	Known  bool
	Values []E
}
type CanonicalExpectation struct{ Output, Failure, Effects []byte }
type Expectation[O, F, E any] struct {
	Output         O
	Failure        F
	Effects        Effects[E]
	Classification string
	Canonical      CanonicalExpectation
}
type WholeWorldDifference struct{ BeforeDigest, AfterDigest string }
type Perturbation[I any] struct {
	ID         string
	Difference WholeWorldDifference
	Apply      func(I) (I, error)
}
type Vector[I, O, F, E any] struct {
	Version, ID, Label               string
	Property                         Property
	Oracle                           Oracle
	Codec                            Codec[I]
	ObservationCodec                 ObservationCodec[O, F, E]
	Valid                            World[I]
	Perturbation                     Perturbation[I]
	ExpectedValid, ExpectedViolating Expectation[O, F, E]
	EvidenceCeiling                  EvidenceCeiling
}
type Observation[O, F, E any] struct {
	Output  O
	Failure F
	Effects []E
}
type Adapter[I, O, F, E any] interface {
	Execute(context.Context, I) (Observation[O, F, E], error)
}
type Classifier[O, F, E any] func(Observation[O, F, E]) (string, error)

type Limits struct{ MaxCanonicalBytes, MaxMismatchBytes, MaxRenderBytes int }

func DefaultLimits() Limits { return Limits{64 << 10, 4 << 10, 64 << 10} }

type Finding struct {
	Code                                     Code `json:"code"`
	World, Surface, Detail, Expected, Actual string
}
type EncodedObservation struct{ Output, Failure, Effects string }
type WorldResult struct {
	World, Classification string
	Observation           EncodedObservation
	Matched               bool
	Findings              []Finding
}
type Status string

const (
	Pass   Status = "PASS"
	Failed Status = "FAILED"
)

type Contrast string

const (
	ContrastEstablished Contrast = "ESTABLISHED"
	ContrastAbsent      Contrast = "ABSENT"
	ContrastInvalid     Contrast = "INVALID"
)

type Report struct {
	Version, VectorID, PropertyID string
	Status                        Status
	Valid, Violating              WorldResult
	Contrast                      Contrast
	Oracle                        Oracle
	OracleQualified               bool
	EvidenceCeiling               EvidenceCeiling
	Findings                      []Finding
	ReproductionID                string
	limits                        Limits
}
type Runner[I, O, F, E any] struct {
	Adapter  Adapter[I, O, F, E]
	Classify Classifier[O, F, E]
	Limits   Limits
}

type prepared struct {
	validBytes, violatingBytes       []byte
	validExpected, violatingExpected CanonicalExpectation
	id                               string
}

func digest(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func decodeCopy[T any](codec Codec[T], canonical []byte) (T, error) {
	return codec.Decode(append([]byte(nil), canonical...))
}
func normalized[E any](v []E) []E {
	if v == nil {
		return []E{}
	}
	return v
}
func stableEncode[T any](surface string, value T, encode func(T) ([]byte, error), max int) ([]byte, error) {
	a, e := encode(value)
	if e != nil {
		return nil, Issue{Code: CodeUnsupported, Surface: surface, Detail: "codec rejected value"}
	}
	b, e := encode(value)
	if e != nil {
		return nil, Issue{Code: CodeUnsupported, Surface: surface, Detail: "codec rejected repeated value"}
	}
	if !bytes.Equal(a, b) {
		return nil, Issue{Code: CodeUnstableEncoding, Surface: surface, Detail: "repeated encoding differed"}
	}
	if len(a) > max {
		return nil, Issue{Code: CodeOverflow, Surface: surface, Detail: "canonical bytes exceed limit"}
	}
	return append([]byte(nil), a...), nil
}
func checkExpected[O, F, E any](x Expectation[O, F, E], c ObservationCodec[O, F, E], max int) (CanonicalExpectation, error) {
	o, e := stableEncode("expectation.output", x.Output, c.EncodeOutput, max)
	if e != nil {
		return CanonicalExpectation{}, e
	}
	f, e := stableEncode("expectation.failure", x.Failure, c.EncodeFailure, max)
	if e != nil {
		return CanonicalExpectation{}, e
	}
	ef, e := stableEncode("expectation.effects", normalized(x.Effects.Values), c.EncodeEffects, max)
	if e != nil {
		return CanonicalExpectation{}, e
	}
	got := CanonicalExpectation{o, f, ef}
	if !bytes.Equal(got.Output, x.Canonical.Output) || !bytes.Equal(got.Failure, x.Canonical.Failure) || !bytes.Equal(got.Effects, x.Canonical.Effects) {
		return CanonicalExpectation{}, Issue{Code: CodeInvalid, Surface: "expectation.canonical", Detail: "supplied canonical expectation bytes do not match codec"}
	}
	return got, nil
}
func (r Runner[I, O, F, E]) limits() Limits {
	l := r.Limits
	if l.MaxCanonicalBytes <= 0 || l.MaxMismatchBytes <= 0 || l.MaxRenderBytes <= 0 {
		return DefaultLimits()
	}
	return l
}
func (r Runner[I, O, F, E]) prepare(v Vector[I, O, F, E]) (prepared, error) {
	var p prepared
	l := r.limits()
	missing := func(surface, detail string) error { return Issue{Code: CodeMissing, Surface: surface, Detail: detail} }
	if v.Version != Version {
		return p, Issue{Code: CodeInvalid, Surface: "version", Detail: "unsupported version"}
	}
	if v.Property.ID == "" || v.Property.Statement == "" || v.Property.Source == "" {
		return p, missing("property", "identity, statement, and source required")
	}
	if v.Oracle.Kind == "" || v.Oracle.Source == "" || v.Oracle.Revision == "" || v.Oracle.Producer == "" || v.Oracle.Method == "" || v.Oracle.Review == "" {
		return p, missing("oracle", "complete provenance and review required")
	}
	switch v.Oracle.Independence {
	case Independent, Diverse, Reference, GeneratorDependent, ImplementationDerived, Unknown:
	default:
		return p, Issue{Code: CodeInvalid, Surface: "oracle.independence", Detail: "invalid independence"}
	}
	if v.EvidenceCeiling.Version == "" || len(v.EvidenceCeiling.CannotEstablish) == 0 {
		return p, missing("evidenceCeiling", "nonempty versioned ceiling required")
	}
	if v.Codec == nil || v.ObservationCodec == nil {
		return p, missing("codec", "world and observation codecs required")
	}
	if v.Perturbation.ID == "" || v.Perturbation.Apply == nil {
		return p, missing("perturbation", "id and mutator required")
	}
	if len(v.Valid.Canonical) == 0 || len(v.Valid.Canonical) > l.MaxCanonicalBytes {
		return p, Issue{Code: CodeOverflow, World: "valid", Surface: "canonical", Detail: "canonical world absent or over limit"}
	}
	if !v.ExpectedValid.Effects.Known || !v.ExpectedViolating.Effects.Known {
		return p, missing("effects", "effects must be known, including zero")
	}
	if v.ExpectedValid.Classification == "" || v.ExpectedViolating.Classification == "" || v.ExpectedValid.Classification == v.ExpectedViolating.Classification {
		return p, Issue{Code: CodeInvalid, Surface: "classification", Detail: "distinct expected classifications required"}
	}
	valid, e := decodeCopy(v.Codec, v.Valid.Canonical)
	if e != nil {
		return p, Issue{Code: CodeUnsupported, World: "valid", Surface: "codec.decode", Detail: "valid canonical bytes rejected"}
	}
	encValid, e := stableEncode("world.valid", valid, v.Codec.Encode, l.MaxCanonicalBytes)
	if e != nil {
		return p, e
	}
	if !bytes.Equal(encValid, v.Valid.Canonical) {
		return p, Issue{Code: CodeInvalid, World: "valid", Surface: "codec", Detail: "decode/encode did not preserve supplied canonical bytes"}
	}
	mutationInput, e := decodeCopy(v.Codec, v.Valid.Canonical)
	if e != nil {
		return p, Issue{Code: CodeUnsupported, World: "violating", Surface: "codec.decode", Detail: "mutation input decode failed"}
	}
	violating, e := v.Perturbation.Apply(mutationInput)
	if e != nil {
		return p, Issue{Code: CodeApply, World: "violating", Surface: "perturbation", Detail: "mutator failed"}
	}
	violatingBytes, e := stableEncode("world.violating", violating, v.Codec.Encode, l.MaxCanonicalBytes)
	if e != nil {
		return p, e
	}
	before, after := digest(v.Valid.Canonical), digest(violatingBytes)
	if before == after {
		return p, Issue{Code: CodeInvalid, World: "violating", Surface: "difference", Detail: "whole-world digest did not change"}
	}
	if v.Perturbation.Difference.BeforeDigest != before || v.Perturbation.Difference.AfterDigest != after {
		return p, Issue{Code: CodeInvalid, World: "violating", Surface: "difference", Detail: "declared whole-world digests do not match canonical content"}
	}
	cev, e := checkExpected(v.ExpectedValid, v.ObservationCodec, l.MaxCanonicalBytes)
	if e != nil {
		return p, e
	}
	cex, e := checkExpected(v.ExpectedViolating, v.ObservationCodec, l.MaxCanonicalBytes)
	if e != nil {
		return p, e
	}
	meta := struct {
		Version          string
		Property         Property
		Oracle           Oracle
		Valid, Violating []byte
		PerturbationID   string
		Difference       WholeWorldDifference
		EV, EX           CanonicalExpectation
		Ceiling          EvidenceCeiling
	}{v.Version, v.Property, v.Oracle, v.Valid.Canonical, violatingBytes, v.Perturbation.ID, v.Perturbation.Difference, cev, cex, v.EvidenceCeiling}
	mb, e := json.Marshal(meta)
	if e != nil {
		return p, e
	}
	id := digest(mb)
	if v.ID != "" && v.ID != id {
		return p, Issue{Code: CodeInvalid, Surface: "identity", Detail: "supplied identity mismatch"}
	}
	return prepared{
		validBytes:        append([]byte(nil), v.Valid.Canonical...),
		violatingBytes:    append([]byte(nil), violatingBytes...),
		validExpected:     cloneExpectation(cev),
		violatingExpected: cloneExpectation(cex),
		id:                id,
	}, nil
}
func cloneExpectation(v CanonicalExpectation) CanonicalExpectation {
	return CanonicalExpectation{
		Output:  append([]byte(nil), v.Output...),
		Failure: append([]byte(nil), v.Failure...),
		Effects: append([]byte(nil), v.Effects...),
	}
}

func (r Runner[I, O, F, E]) Identity(v Vector[I, O, F, E]) (string, error) {
	p, e := r.prepare(v)
	return p.id, e
}
func issueFrom(err error, fallback Code, world, surface string) Finding {
	var i Issue
	if errors.As(err, &i) {
		return Finding{i.Code, i.World, i.Surface, i.Detail, "", ""}
	}
	return Finding{fallback, world, surface, "operation failed", "", ""}
}
func boundedEncoded(b []byte, max int) (string, error) {
	if len(b) > max {
		return "", Issue{Code: CodeOverflow, Surface: "report", Detail: "encoded observation exceeds mismatch limit"}
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}
func encodeObservation[O, F, E any](o Observation[O, F, E], c ObservationCodec[O, F, E], l Limits) (EncodedObservation, error) {
	out, e := stableEncode("observation.output", o.Output, c.EncodeOutput, l.MaxCanonicalBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	fail, e := stableEncode("observation.failure", o.Failure, c.EncodeFailure, l.MaxCanonicalBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	effects, e := stableEncode("observation.effects", normalized(o.Effects), c.EncodeEffects, l.MaxCanonicalBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	a, e := boundedEncoded(out, l.MaxMismatchBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	b, e := boundedEncoded(fail, l.MaxMismatchBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	d, e := boundedEncoded(effects, l.MaxMismatchBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	return EncodedObservation{a, b, d}, nil
}
func expectedEncoded(x CanonicalExpectation, l Limits) (EncodedObservation, error) {
	a, e := boundedEncoded(x.Output, l.MaxMismatchBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	b, e := boundedEncoded(x.Failure, l.MaxMismatchBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	d, e := boundedEncoded(x.Effects, l.MaxMismatchBytes)
	if e != nil {
		return EncodedObservation{}, e
	}
	return EncodedObservation{a, b, d}, nil
}
func compare(world string, actual, expected EncodedObservation, class, expectedClass string) WorldResult {
	w := WorldResult{World: world, Classification: class, Observation: actual, Matched: true}
	pairs := []struct{ s, a, e string }{{"output", actual.Output, expected.Output}, {"failure", actual.Failure, expected.Failure}, {"effects", actual.Effects, expected.Effects}, {"classification", class, expectedClass}}
	for _, p := range pairs {
		if p.a != p.e {
			w.Matched = false
			w.Findings = append(w.Findings, Finding{CodeMismatch, world, p.s, "observed value differs", p.e, p.a})
		}
	}
	sort.Slice(w.Findings, func(i, j int) bool { return w.Findings[i].Surface < w.Findings[j].Surface })
	return w
}
func (r Runner[I, O, F, E]) executeWorld(ctx context.Context, v Vector[I, O, F, E], world string, canonical []byte, expected Expectation[O, F, E], encodedExpected EncodedObservation) (WorldResult, error) {
	value, err := decodeCopy(v.Codec, canonical)
	if err != nil {
		return WorldResult{}, Issue{Code: CodeUnsupported, World: world, Surface: "codec.decode", Detail: "execution decode failed"}
	}
	roundTrip, err := stableEncode("world."+world+".execution", value, v.Codec.Encode, r.limits().MaxCanonicalBytes)
	if err != nil {
		return WorldResult{}, err
	}
	if !bytes.Equal(roundTrip, canonical) {
		return WorldResult{}, Issue{Code: CodeInvalid, World: world, Surface: "codec", Detail: "execution round-trip changed canonical bytes"}
	}
	observation, err := r.Adapter.Execute(ctx, value)
	if err != nil {
		return WorldResult{}, Issue{Code: CodeAdapter, World: world, Surface: "adapter", Detail: "adapter failed"}
	}
	classification, err := r.Classify(observation)
	if err != nil {
		return WorldResult{}, Issue{Code: CodeClassifier, World: world, Surface: "classifier", Detail: "classifier failed"}
	}
	encoded, err := encodeObservation(observation, v.ObservationCodec, r.limits())
	if err != nil {
		return WorldResult{}, err
	}
	return compare(world, encoded, encodedExpected, classification, expected.Classification), nil
}

func (r Runner[I, O, F, E]) Run(ctx context.Context, v Vector[I, O, F, E]) Report {
	l := r.limits()
	rep := Report{Version: Version, VectorID: v.ID, PropertyID: v.Property.ID, Status: Failed, Contrast: ContrastInvalid, Oracle: v.Oracle, OracleQualified: v.Oracle.Qualified(), EvidenceCeiling: v.EvidenceCeiling, limits: l}
	if !rep.OracleQualified {
		rep.Findings = append(rep.Findings, Finding{Code: CodeOracleUnqualified, Surface: "oracle.independence", Detail: "oracle cannot establish independent conformance"})
	}
	if r.Adapter == nil {
		rep.Findings = append(rep.Findings, Finding{Code: CodeMissing, Surface: "adapter", Detail: "adapter required"})
		return rep
	}
	if r.Classify == nil {
		rep.Findings = append(rep.Findings, Finding{Code: CodeMissing, Surface: "classifier", Detail: "classifier required"})
		return rep
	}
	p, e := r.prepare(v)
	if e != nil {
		rep.Findings = append(rep.Findings, issueFrom(e, CodeInvalid, "", "validation"))
		return rep
	}
	rep.VectorID = p.id
	rep.ReproductionID = p.id
	eev, e := expectedEncoded(p.validExpected, l)
	if e != nil {
		rep.Findings = append(rep.Findings, issueFrom(e, CodeOverflow, "valid", "report"))
		return rep
	}
	eex, e := expectedEncoded(p.violatingExpected, l)
	if e != nil {
		rep.Findings = append(rep.Findings, issueFrom(e, CodeOverflow, "violating", "report"))
		return rep
	}
	rep.Valid, e = r.executeWorld(ctx, v, "valid", p.validBytes, v.ExpectedValid, eev)
	if e != nil {
		rep.Findings = append(rep.Findings, issueFrom(e, CodeInvalid, "valid", "execution"))
		return rep
	}
	// The valid observation is now fully classified and copied into library-owned
	// encoded bytes. Only now may a codec decode the violating generic value.
	rep.Violating, e = r.executeWorld(ctx, v, "violating", p.violatingBytes, v.ExpectedViolating, eex)
	if e != nil {
		rep.Findings = append(rep.Findings, issueFrom(e, CodeInvalid, "violating", "execution"))
		return rep
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
	if len(rep.Findings) == 0 {
		rep.Status = Pass
	}
	return rep
}
func (r Report) dto() any {
	return struct {
		Version, VectorID, PropertyID string
		Status                        Status
		Valid, Violating              WorldResult
		Contrast                      Contrast
		Oracle                        Oracle
		OracleQualified               bool
		EvidenceCeiling               EvidenceCeiling
		Findings                      []Finding
		ReproductionID                string
	}{r.Version, r.VectorID, r.PropertyID, r.Status, r.Valid, r.Violating, r.Contrast, r.Oracle, r.OracleQualified, r.EvidenceCeiling, r.Findings, r.ReproductionID}
}
func (r Report) Structured() ([]byte, error) {
	b, e := json.Marshal(r.dto())
	if e != nil {
		return nil, e
	}
	limit := r.limits.MaxRenderBytes
	if limit <= 0 {
		limit = DefaultLimits().MaxRenderBytes
	}
	if len(b) > limit {
		return nil, Issue{Code: CodeOverflow, Surface: "report.structured", Detail: "render exceeds limit"}
	}
	return b, nil
}
func (r Report) Human() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s vector %s\nproperty: %s\nstatus: %s\ncontrast: %s\noracle-qualified: %t\n", r.Version, r.VectorID, r.PropertyID, r.Status, r.Contrast, r.OracleQualified)
	fmt.Fprintf(&b, "evidence ceiling: %s\nreplay: %s\n", strings.Join(r.EvidenceCeiling.CannotEstablish, ", "), r.ReproductionID)
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "- %s %s %s: %s", f.Code, f.World, f.Surface, f.Detail)
		if f.Expected != "" || f.Actual != "" {
			fmt.Fprintf(&b, " expected=%s actual=%s", f.Expected, f.Actual)
		}
		b.WriteByte('\n')
	}
	limit := r.limits.MaxRenderBytes
	if limit <= 0 {
		limit = DefaultLimits().MaxRenderBytes
	}
	if b.Len() > limit {
		return "", Issue{Code: CodeOverflow, Surface: "report.human", Detail: "render exceeds limit"}
	}
	return b.String(), nil
}
