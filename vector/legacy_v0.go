package vector

import (
	"bytes"
	"encoding/json"
	"errors"
)

type legacyV0Oracle struct {
	Kind, Source, Revision, Producer, Method, Review string
	Independence                                     Independence
}

type legacyV0Prepared struct {
	validBytes, violatingBytes       []byte
	validExpected, violatingExpected CanonicalExpectation
	id                               string
}

func hasV1VectorFields[I, O, F, E any](v Vector[I, O, F, E]) bool {
	return v.Oracle.Verification.Status != "" || len(v.Oracle.Verification.Evidence) != 0 ||
		v.ExpectedValid.FailureExpectation != (FailureExpectation{}) ||
		v.ExpectedViolating.FailureExpectation != (FailureExpectation{}) ||
		v.Executables != (ExecutableCustody{})
}

// prepareLegacyV0 is the historical v0 validator and canonical identity path.
// It intentionally does not call the v1 prepare implementation.
func prepareLegacyV0[I, O, F, E any](v Vector[I, O, F, E], limits Limits) (legacyV0Prepared, error) {
	var p legacyV0Prepared
	if v.Version != VersionV0 {
		return p, Issue{Code: CodeInvalid, Surface: "version", Detail: "legacy identity requires semantic-vector/v0"}
	}
	if hasV1VectorFields(v) {
		return p, errors.Join(ErrLegacyVersion, Issue{Code: CodeInvalid, Surface: "version", Detail: "v1-only metadata is ambiguous in v0"})
	}
	if limits.MaxCanonicalBytes <= 0 || limits.MaxMismatchBytes <= 0 || limits.MaxRenderBytes <= 0 {
		limits = DefaultLimits()
	}
	missing := func(surface, detail string) error { return Issue{Code: CodeMissing, Surface: surface, Detail: detail} }
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
	if len(v.Valid.Canonical) == 0 || len(v.Valid.Canonical) > limits.MaxCanonicalBytes {
		return p, Issue{Code: CodeOverflow, World: "valid", Surface: "canonical", Detail: "canonical world absent or over limit"}
	}
	if !v.ExpectedValid.Effects.Known || !v.ExpectedViolating.Effects.Known {
		return p, missing("effects", "effects must be known, including zero")
	}
	if v.ExpectedValid.Classification == "" || v.ExpectedViolating.Classification == "" || v.ExpectedValid.Classification == v.ExpectedViolating.Classification {
		return p, Issue{Code: CodeInvalid, Surface: "classification", Detail: "distinct expected classifications required"}
	}
	valid, err := decodeCopy(v.Codec, v.Valid.Canonical)
	if err != nil {
		return p, Issue{Code: CodeUnsupported, World: "valid", Surface: "codec.decode", Detail: "valid canonical bytes rejected"}
	}
	encodedValid, err := stableEncode("world.valid", valid, v.Codec.Encode, limits.MaxCanonicalBytes)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(encodedValid, v.Valid.Canonical) {
		return p, Issue{Code: CodeInvalid, World: "valid", Surface: "codec", Detail: "decode/encode did not preserve supplied canonical bytes"}
	}
	mutationInput, err := decodeCopy(v.Codec, v.Valid.Canonical)
	if err != nil {
		return p, Issue{Code: CodeUnsupported, World: "violating", Surface: "codec.decode", Detail: "mutation input decode failed"}
	}
	violating, err := v.Perturbation.Apply(mutationInput)
	if err != nil {
		return p, Issue{Code: CodeApply, World: "violating", Surface: "perturbation", Detail: "mutator failed"}
	}
	violatingBytes, err := stableEncode("world.violating", violating, v.Codec.Encode, limits.MaxCanonicalBytes)
	if err != nil {
		return p, err
	}
	before, after := digest(v.Valid.Canonical), digest(violatingBytes)
	if before == after {
		return p, Issue{Code: CodeInvalid, World: "violating", Surface: "difference", Detail: "whole-world digest did not change"}
	}
	if v.Perturbation.Difference.BeforeDigest != before || v.Perturbation.Difference.AfterDigest != after {
		return p, Issue{Code: CodeInvalid, World: "violating", Surface: "difference", Detail: "declared whole-world digests do not match canonical content"}
	}
	validExpected, err := checkExpected(v.ExpectedValid, v.ObservationCodec, limits.MaxCanonicalBytes)
	if err != nil {
		return p, err
	}
	violatingExpected, err := checkExpected(v.ExpectedViolating, v.ObservationCodec, limits.MaxCanonicalBytes)
	if err != nil {
		return p, err
	}
	meta := struct {
		Version          string
		Property         Property
		Oracle           legacyV0Oracle
		Valid, Violating []byte
		PerturbationID   string
		Difference       WholeWorldDifference
		EV, EX           CanonicalExpectation
		Ceiling          EvidenceCeiling
	}{
		Version:  VersionV0,
		Property: v.Property,
		Oracle:   legacyV0Oracle{Kind: v.Oracle.Kind, Source: v.Oracle.Source, Revision: v.Oracle.Revision, Producer: v.Oracle.Producer, Method: v.Oracle.Method, Review: v.Oracle.Review, Independence: v.Oracle.Independence},
		Valid:    append([]byte(nil), v.Valid.Canonical...), Violating: append([]byte(nil), violatingBytes...),
		PerturbationID: v.Perturbation.ID, Difference: v.Perturbation.Difference,
		EV: cloneExpectation(validExpected), EX: cloneExpectation(violatingExpected),
		Ceiling: EvidenceCeiling{Version: v.EvidenceCeiling.Version, CannotEstablish: append([]string(nil), v.EvidenceCeiling.CannotEstablish...)},
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return p, err
	}
	p = legacyV0Prepared{validBytes: append([]byte(nil), v.Valid.Canonical...), violatingBytes: append([]byte(nil), violatingBytes...), validExpected: cloneExpectation(validExpected), violatingExpected: cloneExpectation(violatingExpected), id: digest(encoded)}
	if v.ID != "" && v.ID != p.id {
		return legacyV0Prepared{}, Issue{Code: CodeInvalid, Surface: "identity", Detail: "supplied identity mismatch"}
	}
	return p, nil
}

// LegacyV0Identity reproduces the historical v0 identity metadata shape.
func LegacyV0Identity[I, O, F, E any](v Vector[I, O, F, E], limits Limits) (string, error) {
	prepared, err := prepareLegacyV0(v, limits)
	return prepared.id, err
}

// LegacyV0StructuredReport reproduces the historical v0 report JSON field set.
// It does not execute v0 vectors or reinterpret v1 report classification.
func LegacyV0StructuredReport(report Report) ([]byte, error) {
	if report.Version != VersionV0 {
		return nil, Issue{Code: CodeInvalid, Surface: "report.version", Detail: "legacy report encoding requires semantic-vector/v0"}
	}
	if report.Oracle.Verification.Status != "" || len(report.Oracle.Verification.Evidence) != 0 || report.Classification != (ReportClassification{}) || report.AttributionClaim != nil || report.Binding != (BindingCustody{}) {
		return nil, errors.Join(ErrLegacyVersion, Issue{Code: CodeInvalid, Surface: "report.version", Detail: "v1-only report fields are ambiguous in v0"})
	}
	oracle := legacyV0Oracle{Kind: report.Oracle.Kind, Source: report.Oracle.Source, Revision: report.Oracle.Revision, Producer: report.Oracle.Producer, Method: report.Oracle.Method, Review: report.Oracle.Review, Independence: report.Oracle.Independence}
	return json.Marshal(struct {
		Version, VectorID, PropertyID string
		Status                        Status
		Valid, Violating              WorldResult
		Contrast                      Contrast
		Oracle                        legacyV0Oracle
		OracleQualified               bool
		EvidenceCeiling               EvidenceCeiling
		Findings                      []Finding
		ReproductionID                string
	}{report.Version, report.VectorID, report.PropertyID, report.Status, report.Valid, report.Violating, report.Contrast, oracle, report.OracleQualified, report.EvidenceCeiling, report.Findings, report.ReproductionID})
}
