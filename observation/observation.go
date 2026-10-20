// Package observation assesses caller-supplied execution observations without
// owning or claiming execution. Its identity domain is semantic-vector-observation/v1.
package observation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Version is the exact observation schema and identity-domain version.
const Version = "semantic-vector-observation/v1"

// ErrInvalid is the sentinel wrapped by typed observation validation issues.
var ErrInvalid = errors.New("invalid observation specification")

// Code classifies an observation validation issue.
type Code string

const (
	// CodeMissing reports a required value that was not supplied.
	CodeMissing Code = "MISSING"
	// CodeInvalid reports a value that violates the observation contract.
	CodeInvalid Code = "INVALID"
	// CodeDuplicate reports a repeated identity.
	CodeDuplicate Code = "DUPLICATE_IDENTITY"
	// CodeUnstable reports a codec that does not reproduce canonical bytes.
	CodeUnstable Code = "UNSTABLE_CODEC"
	// CodeCodecPanic reports a contained codec panic.
	CodeCodecPanic Code = "CODEC_PANIC"
	// CodeOverflow reports a configured size or work limit violation.
	CodeOverflow Code = "OVERFLOW"
	// CodeMismatch reports supplied evidence that does not bind to the specification.
	CodeMismatch Code = "OBSERVATION_MISMATCH"
)

// Issue describes one typed observation validation failure.
type Issue struct {
	// Code classifies the failure.
	Code Code `json:"code"`
	// Surface identifies the rejected input surface.
	Surface string
	// Detail explains the rejection without granting evidence authority.
	Detail string
}

// Error formats the typed issue.
func (i Issue) Error() string { return fmt.Sprintf("%s surface=%q: %s", i.Code, i.Surface, i.Detail) }

// Unwrap exposes ErrInvalid for errors.Is checks.
func (i Issue) Unwrap() error { return ErrInvalid }

// Limits bounds canonical bytes, report bytes, rows, and work.
type Limits struct {
	// MaxCanonicalBytes bounds each canonical input and frozen specification.
	MaxCanonicalBytes int
	// MaxReportBytes bounds a canonical report.
	MaxReportBytes int
	// MaxRows bounds specification rows and assessment batches.
	MaxRows int
	// MaxWork bounds validation and assessment work units.
	MaxWork int
}

// DefaultLimits returns conservative library limits.
func DefaultLimits() Limits { return Limits{64 << 10, 1 << 20, 1024, 10000} }

// Codec deterministically encodes an input as compact canonical JSON.
type Codec[I any] interface {
	// Encode returns compact canonical JSON for v.
	Encode(v I) ([]byte, error)
}

// Property identifies a declared property and its source revision.
type Property struct {
	// ID is the property identity and must match its row identity.
	ID string
	// Statement is the declared property text.
	Statement string
	// Source identifies the property's external or local source.
	Source string
	// Version identifies the source revision.
	Version string
}

// ByteSpan identifies corresponding changed byte ranges in two canonical inputs.
type ByteSpan struct {
	// BeforeStart and BeforeEnd delimit the changed range in the valid input.
	BeforeStart, BeforeEnd int
	// AfterStart and AfterEnd delimit the changed range in the violating input.
	AfterStart, AfterEnd int
}

// Difference binds one declared property to canonical input digests and changed spans.
type Difference struct {
	// PropertyID identifies the property under comparison.
	PropertyID string
	// BeforeDigest and AfterDigest bind the valid and violating canonical inputs.
	BeforeDigest, AfterDigest string
	// Claims contains the single declared controlled difference.
	Claims []string
	// Spans enumerate all changed canonical byte ranges.
	Spans []ByteSpan
	// PropertyStatus records the limited verification status of the property declaration.
	PropertyStatus string
}

const (
	// PropertyDeclaredUnverified marks a property as declared but not established by this library.
	PropertyDeclaredUnverified = "DECLARED_UNVERIFIED"
	// ByteDifferenceWitnessVerified marks only that the declared canonical byte difference was verified.
	ByteDifferenceWitnessVerified = "BYTE_DIFFERENCE_WITNESS_VERIFIED"
)

// Outcome is a canonical expected output, failure class, and effects tuple.
type Outcome struct {
	// Output is the canonical output bytes.
	Output []byte
	// FailureClass is the canonical failure classification bytes.
	FailureClass []byte
	// Effects is the canonical effects bytes; an empty non-nil value is explicit.
	Effects []byte
}

// Expected pairs outcomes for the valid and violating inputs.
type Expected struct {
	// Valid is the expected outcome for the valid input.
	Valid Outcome
	// Violating is the expected outcome for the violating input.
	Violating Outcome
}

// ReviewEvidence records externally supplied review provenance.
type ReviewEvidence struct {
	// Reviewer identifies the reviewer.
	Reviewer string
	// Scope identifies what the reviewer examined.
	Scope string
	// Source identifies the reviewed source.
	Source string
	// Revision identifies the reviewed source revision.
	Revision string
}

// Oracle describes the provenance and review status of expected outcomes.
type Oracle struct {
	// Kind identifies the oracle category.
	Kind string
	// Source identifies the oracle source.
	Source string
	// Revision identifies the oracle source revision.
	Revision string
	// Producer identifies who produced the oracle.
	Producer string
	// Method identifies how the oracle was produced.
	Method string
	// Independence describes separation from the implementation under assessment.
	Independence string
	// Verification records the claimed review status without granting authority.
	Verification string
	// Evidence contains externally supplied review provenance.
	Evidence []ReviewEvidence
}

// EvidenceCeiling declares claims that the supplied evidence cannot establish.
type EvidenceCeiling struct {
	// Version identifies the ceiling schema.
	Version string
	// CannotEstablish lists excluded claims, such as causality or production behavior.
	CannotEstablish []string
}

// MutationIdentity binds an optional named mutation to one property's canonical worlds.
type MutationIdentity struct {
	// ID identifies the mutation.
	ID string
	// PropertyID identifies the property changed by the mutation.
	PropertyID string
	// Before and After are the canonical valid and violating inputs.
	Before, After []byte
}

// Row defines one controlled valid-versus-violating comparison.
type Row[I any] struct {
	// ID is the row identity.
	ID string
	// Property is the declared property under comparison.
	Property Property
	// Violating is the typed violating input.
	Violating I
	// Difference declares the controlled canonical input difference.
	Difference Difference
	// Expected contains externally authored expected outcomes.
	Expected Expected
	// Mutation optionally binds a named mutation to the canonical inputs.
	Mutation *MutationIdentity
}

// ObservationSpecification defines typed inputs, expected outcomes, provenance, and evidence limits.
type ObservationSpecification[I any] struct {
	// Version must equal Version.
	Version string
	// CodecKey identifies the canonical codec.
	CodecKey string
	// Codec encodes typed inputs as compact canonical JSON.
	Codec Codec[I]
	// Valid is the typed valid input.
	Valid I
	// Rows are strictly identity-ordered controlled comparisons.
	Rows []Row[I]
	// Oracle records externally supplied expected-outcome provenance.
	Oracle Oracle
	// EvidenceCeiling limits what assessment may claim.
	EvidenceCeiling EvidenceCeiling
	// Limits bounds freezing and assessment.
	Limits Limits
}

type frozenRow struct {
	ID         string            `json:"id"`
	Property   Property          `json:"property"`
	Violating  []byte            `json:"violating"`
	Difference Difference        `json:"difference"`
	Expected   Expected          `json:"expected"`
	Mutation   *MutationIdentity `json:"mutation,omitempty"`
}
type frozenWire struct {
	Version         string          `json:"version"`
	CodecKey        string          `json:"codec_key"`
	Valid           []byte          `json:"valid"`
	Rows            []frozenRow     `json:"rows"`
	Oracle          Oracle          `json:"oracle"`
	EvidenceCeiling EvidenceCeiling `json:"evidence_ceiling"`
	Limits          Limits          `json:"limits"`
}

// FrozenObservationSpecification is an immutable canonical specification bound to an identity.
type FrozenObservationSpecification struct {
	wire      frozenWire
	canonical []byte
	identity  string
}

// Identity returns the frozen specification identity.
func (f FrozenObservationSpecification) Identity() string { return f.identity }

// Canonical returns a copy of the frozen specification's canonical bytes.
func (f FrozenObservationSpecification) Canonical() []byte {
	return append([]byte(nil), f.canonical...)
}

func digest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func clone(b []byte) []byte  { return append([]byte(nil), b...) }
func cloneOutcome(x Outcome) Outcome {
	return Outcome{clone(x.Output), clone(x.FailureClass), clone(x.Effects)}
}
func validDigest(s string) bool {
	if len(s) != 71 || s[:7] != "sha256:" {
		return false
	}
	_, e := hex.DecodeString(s[7:])
	return e == nil
}
func limitsOK(l Limits) bool {
	return l.MaxCanonicalBytes > 0 && l.MaxReportBytes > 0 && l.MaxRows > 0 && l.MaxWork > 0
}
func checkedAdd(a, b int) (int, bool) {
	if a < 0 || b < 0 || a > int(^uint(0)>>1)-b {
		return 0, false
	}
	return a + b, true
}
func verifyByteDifference(before, after []byte, d Difference) error {
	if d.PropertyStatus != PropertyDeclaredUnverified || len(d.Spans) == 0 || bytes.Equal(before, after) {
		return Issue{Code: CodeInvalid, Surface: "difference.witness", Detail: "declared-unverified non-identical byte witness required"}
	}
	bp, ap := 0, 0
	for _, s := range d.Spans {
		if s.BeforeStart < bp || s.AfterStart < ap || s.BeforeStart >= s.BeforeEnd || s.AfterStart >= s.AfterEnd || s.BeforeEnd > len(before) || s.AfterEnd > len(after) {
			return Issue{Code: CodeInvalid, Surface: "difference.witness", Detail: "spans must be ordered, nonoverlapping, nonempty, and in bounds"}
		}
		if !bytes.Equal(before[bp:s.BeforeStart], after[ap:s.AfterStart]) {
			return Issue{Code: CodeInvalid, Surface: "difference.witness", Detail: "bytes outside declared spans differ"}
		}
		bp, ap = s.BeforeEnd, s.AfterEnd
	}
	if !bytes.Equal(before[bp:], after[ap:]) {
		return Issue{Code: CodeInvalid, Surface: "difference.witness", Detail: "bytes outside declared spans differ"}
	}
	return nil
}

func callEncode[I any](c Codec[I], v I) (out []byte, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = Issue{Code: CodeCodecPanic, Surface: "codec", Detail: "codec panicked"}
		}
	}()
	out, err = c.Encode(v)
	if err != nil {
		return nil, Issue{Code: CodeInvalid, Surface: "codec", Detail: "codec rejected value"}
	}
	return out, nil
}
func stable[I any](c Codec[I], v I, max int) ([]byte, error) {
	a, e := callEncode(c, v)
	if e != nil {
		return nil, e
	}
	b, e := callEncode(c, v)
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(a, b) {
		return nil, Issue{Code: CodeUnstable, Surface: "codec", Detail: "repeated encoding differed"}
	}
	if len(a) > max {
		return nil, Issue{Code: CodeOverflow, Surface: "codec", Detail: "canonical bytes exceed limit"}
	}
	if !json.Valid(a) {
		return nil, Issue{Code: CodeInvalid, Surface: "codec", Detail: "canonical input is not JSON"}
	}
	var compact bytes.Buffer
	if e = json.Compact(&compact, a); e != nil || !bytes.Equal(compact.Bytes(), a) {
		return nil, Issue{Code: CodeInvalid, Surface: "codec", Detail: "input is not compact canonical JSON"}
	}
	return clone(a), nil
}
func oracleQualified(o Oracle) bool {
	if o.Verification != "INDEPENDENTLY_REVIEWED" || o.Independence == "" || o.Independence == "unknown" || o.Independence == "implementation-derived" || len(o.Evidence) == 0 {
		return false
	}
	for _, e := range o.Evidence {
		if e.Reviewer == "" || e.Scope == "" || e.Source == "" || e.Revision == "" {
			return false
		}
	}
	return true
}
func outcomeComplete(o Outcome) bool {
	return o.Output != nil && o.FailureClass != nil && o.Effects != nil
}

// FreezeObservationSpecification validates, canonicalizes, and defensively copies s.
// It binds declared expectations and provenance but does not execute the subject
// or establish causality, production behavior, or authority.
func FreezeObservationSpecification[I any](s ObservationSpecification[I]) (FrozenObservationSpecification, error) {
	if s.Version != Version {
		return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: "version", Detail: "exact observation version required"}
	}
	if !limitsOK(s.Limits) {
		return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: "limits", Detail: "positive limits required"}
	}
	if s.Codec == nil || s.CodecKey == "" {
		return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: "codec", Detail: "stable codec and key required"}
	}
	if len(s.Rows) == 0 {
		return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: "rows", Detail: "at least one violating row required"}
	}
	if len(s.Rows) > s.Limits.MaxRows {
		return FrozenObservationSpecification{}, Issue{Code: CodeOverflow, Surface: "rows", Detail: "row limit exceeded"}
	}
	freezeWork, ok := checkedAdd(len(s.Rows), 1)
	if !ok || freezeWork > s.Limits.MaxWork {
		return FrozenObservationSpecification{}, Issue{Code: CodeOverflow, Surface: "work", Detail: "work limit exceeded"}
	}
	valid, e := stable(s.Codec, s.Valid, s.Limits.MaxCanonicalBytes)
	if e != nil {
		return FrozenObservationSpecification{}, e
	}
	if s.Oracle.Kind == "" || s.Oracle.Source == "" || s.Oracle.Revision == "" || s.Oracle.Producer == "" || s.Oracle.Method == "" {
		return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: "oracle.provenance", Detail: "complete oracle provenance required"}
	}
	if s.Oracle.Verification == "" {
		return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: "oracle.verification", Detail: "verification status required"}
	}
	if s.EvidenceCeiling.Version == "" || len(s.EvidenceCeiling.CannotEstablish) == 0 {
		return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: "evidence_ceiling", Detail: "nonempty ceiling required"}
	}
	rows := make([]frozenRow, 0, len(s.Rows))
	ids := map[string]bool{}
	for i, r := range s.Rows {
		surface := fmt.Sprintf("rows[%d]", i)
		if r.ID == "" || r.Property.ID == "" || r.Property.Statement == "" || r.Property.Source == "" || r.Property.Version == "" {
			return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: surface, Detail: "row and property identity required"}
		}
		if ids[r.ID] {
			return FrozenObservationSpecification{}, Issue{Code: CodeDuplicate, Surface: surface, Detail: "duplicate row identity"}
		}
		ids[r.ID] = true
		if r.Property.ID != r.ID || r.Difference.PropertyID != r.Property.ID || len(r.Difference.Claims) != 1 || !validDigest(r.Difference.BeforeDigest) || !validDigest(r.Difference.AfterDigest) {
			return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: surface + ".difference", Detail: "controlled difference must bind property and canonical digests"}
		}
		viol, e := stable(s.Codec, r.Violating, s.Limits.MaxCanonicalBytes)
		if e != nil {
			return FrozenObservationSpecification{}, e
		}
		if bytes.Equal(valid, viol) || r.Difference.BeforeDigest == r.Difference.AfterDigest {
			return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: surface + ".worlds", Detail: "valid and violating worlds must differ"}
		}
		if digest(valid) != r.Difference.BeforeDigest || digest(viol) != r.Difference.AfterDigest {
			return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: surface + ".difference", Detail: "difference digest mismatch"}
		}
		if e := verifyByteDifference(valid, viol, r.Difference); e != nil {
			return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: surface + ".difference", Detail: e.Error()}
		}
		if !outcomeComplete(r.Expected.Valid) || !outcomeComplete(r.Expected.Violating) {
			return FrozenObservationSpecification{}, Issue{Code: CodeMissing, Surface: surface + ".expected", Detail: "output failure class and effects must be explicit"}
		}
		var m *MutationIdentity
		if r.Mutation != nil {
			x := *r.Mutation
			x.Before = clone(x.Before)
			x.After = clone(x.After)
			if x.ID == "" || x.PropertyID != r.Property.ID || !bytes.Equal(x.Before, valid) || !bytes.Equal(x.After, viol) {
				return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: surface + ".mutation", Detail: "mutation identity must bind property and canonical before/after bytes"}
			}
			m = &x
		}
		rows = append(rows, frozenRow{r.ID, r.Property, viol, Difference{PropertyID: r.Difference.PropertyID, BeforeDigest: r.Difference.BeforeDigest, AfterDigest: r.Difference.AfterDigest, Claims: append([]string(nil), r.Difference.Claims...), Spans: append([]ByteSpan(nil), r.Difference.Spans...), PropertyStatus: r.Difference.PropertyStatus}, Expected{cloneOutcome(r.Expected.Valid), cloneOutcome(r.Expected.Violating)}, m})
	}
	if !sort.SliceIsSorted(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID }) {
		return FrozenObservationSpecification{}, Issue{Code: CodeInvalid, Surface: "rows", Detail: "rows must be strictly ordered by identity"}
	}
	w := frozenWire{Version, s.CodecKey, valid, rows, s.Oracle, EvidenceCeiling{s.EvidenceCeiling.Version, append([]string(nil), s.EvidenceCeiling.CannotEstablish...)}, s.Limits}
	w.Oracle.Evidence = append([]ReviewEvidence(nil), s.Oracle.Evidence...)
	canonical, e := json.Marshal(w)
	if e != nil {
		return FrozenObservationSpecification{}, e
	}
	if len(canonical) > s.Limits.MaxCanonicalBytes {
		return FrozenObservationSpecification{}, Issue{Code: CodeOverflow, Surface: "specification", Detail: "canonical specification exceeds limit"}
	}
	return FrozenObservationSpecification{w, canonical, digest(append([]byte(Version+"\x00"), canonical...))}, nil
}

// ObservedOutcome is one externally supplied observed outcome tuple.
type ObservedOutcome struct {
	// Output, FailureClass, and Effects are externally supplied canonical values.
	Output, FailureClass, Effects []byte `json:",omitempty"`
}

// ObservedRow is one externally supplied observation of a specification row.
type ObservedRow struct {
	// ID identifies the observed row.
	ID string `json:"id"`
	// ViolatingInput is the externally supplied canonical violating input.
	ViolatingInput []byte `json:"violating_input"`
	// Valid and Violating are externally supplied outcomes for the two worlds.
	Valid, Violating ObservedOutcome
	// ReceiptDigest and ReceiptProvenance identify optional external custody evidence.
	ReceiptDigest, ReceiptProvenance string
}

// ExternalObservation contains caller-supplied evidence held in external custody.
// Supplying it does not establish execution causality, production behavior, or authority.
type ExternalObservation struct {
	// Version and SpecificationIdentity bind the observation to a frozen specification.
	Version, SpecificationIdentity string
	// ValidInput is the externally supplied canonical valid input.
	ValidInput []byte
	// Rows are the externally supplied row observations.
	Rows []ObservedRow
}

// Classification describes how supplied evidence compares with a frozen specification.
type Classification string

const (
	// SpecInvalid indicates an invalid frozen specification.
	SpecInvalid Classification = "SPEC_INVALID"
	// ObservationMalformed indicates malformed caller-supplied evidence.
	ObservationMalformed Classification = "OBSERVATION_MALFORMED"
	// ObservationMismatch indicates evidence that does not bind or match.
	ObservationMismatch Classification = "OBSERVATION_MISMATCH"
	// SuppliedOutcomesMatchQualifiedSpecification indicates a canonical match to qualified declarations, not causal proof.
	SuppliedOutcomesMatchQualifiedSpecification Classification = "SUPPLIED_OUTCOMES_MATCH_QUALIFIED_SPECIFICATION"
	// NonDiscriminating indicates equal supplied outcomes for valid and violating inputs.
	NonDiscriminating Classification = "NON_DISCRIMINATING"
	// Unqualified indicates matching supplied outcomes whose oracle lacks qualification.
	Unqualified Classification = "UNQUALIFIED"
)

// Report is a deterministic assessment of externally supplied evidence.
// It records external custody and noncausality and grants no acceptance authority.
type Report struct {
	// Version, Identity, and SpecificationIdentity bind the report schema and inputs.
	Version, Identity, SpecificationIdentity string
	// Classification states the bounded comparison result.
	Classification Classification
	// ExecutionCustody, ObservationBinding, and CausalExecution state custody and binding limits.
	ExecutionCustody, ObservationBinding, CausalExecution string
	// DifferenceWitness states the verified canonical-difference status.
	DifferenceWitness string
	// Authority is always zero; the report grants no decision authority.
	Authority int
	// Accepted is always false; assessment is not acceptance.
	Accepted bool
	// OracleStatus repeats the frozen oracle's claimed review status.
	OracleStatus string
	// Findings contains typed bounded assessment issues.
	Findings []Issue
}

func same(a ObservedOutcome, b Outcome) bool {
	return bytes.Equal(a.Output, b.Output) && bytes.Equal(a.FailureClass, b.FailureClass) && bytes.Equal(a.Effects, b.Effects)
}
func equalObserved(a, b ObservedOutcome) bool {
	return bytes.Equal(a.Output, b.Output) && bytes.Equal(a.FailureClass, b.FailureClass) && bytes.Equal(a.Effects, b.Effects)
}
func assess(f FrozenObservationSpecification, o ExternalObservation) (Report, error) {
	r := Report{Version: Version, SpecificationIdentity: f.identity, ExecutionCustody: "EXTERNAL", ObservationBinding: "CANONICALLY_VERIFIED", CausalExecution: "NOT_OBSERVED", DifferenceWitness: ByteDifferenceWitnessVerified, Authority: 0, Accepted: false, OracleStatus: f.wire.Oracle.Verification, Findings: []Issue{}}
	if f.identity == "" {
		r.Classification = SpecInvalid
		r.ObservationBinding = "UNVERIFIED"
		return finish(r, f.wire.Limits)
	}
	if o.Version != Version || o.SpecificationIdentity != f.identity || o.ValidInput == nil || o.Rows == nil {
		r.Classification = ObservationMalformed
		r.ObservationBinding = "UNVERIFIED"
		return finish(r, f.wire.Limits)
	}
	if !bytes.Equal(o.ValidInput, f.wire.Valid) || len(o.Rows) != len(f.wire.Rows) {
		r.Classification = ObservationMismatch
		r.ObservationBinding = "MISMATCH"
		return finish(r, f.wire.Limits)
	}
	qualified := oracleQualified(f.wire.Oracle)
	allMatched := true
	discriminating := true
	for i, x := range o.Rows {
		want := f.wire.Rows[i]
		if x.ID != want.ID || !bytes.Equal(x.ViolatingInput, want.Violating) {
			r.Classification = ObservationMismatch
			r.ObservationBinding = "MISMATCH"
			return finish(r, f.wire.Limits)
		}
		if x.Valid.Output == nil || x.Valid.FailureClass == nil || x.Valid.Effects == nil || x.Violating.Output == nil || x.Violating.FailureClass == nil || x.Violating.Effects == nil || (x.ReceiptDigest != "" && !validDigest(x.ReceiptDigest)) {
			r.Classification = ObservationMalformed
			r.ObservationBinding = "UNVERIFIED"
			return finish(r, f.wire.Limits)
		}
		allMatched = allMatched && same(x.Valid, want.Expected.Valid) && same(x.Violating, want.Expected.Violating)
		discriminating = discriminating && !equalObserved(x.Valid, x.Violating)
	}
	switch {
	case !discriminating:
		r.Classification = NonDiscriminating
	case !allMatched:
		r.Classification = ObservationMismatch
	case !qualified:
		r.Classification = Unqualified
	default:
		r.Classification = SuppliedOutcomesMatchQualifiedSpecification
	}
	return finish(r, f.wire.Limits)
}
func finish(r Report, l Limits) (Report, error) {
	raw, _ := json.Marshal(struct {
		Version                  string
		Specification            string
		Classification           Classification
		Custody, Binding, Causal string
		DifferenceWitness        string
		Authority                int
		Accepted                 bool
		Oracle                   string
		Findings                 []Issue
	}{r.Version, r.SpecificationIdentity, r.Classification, r.ExecutionCustody, r.ObservationBinding, r.CausalExecution, r.DifferenceWitness, r.Authority, r.Accepted, r.OracleStatus, r.Findings})
	r.Identity = digest(append([]byte(Version+"/report\x00"), raw...))
	if l.MaxReportBytes > 0 && len(raw) > l.MaxReportBytes {
		return Report{}, Issue{Code: CodeOverflow, Surface: "report", Detail: "report exceeds limit"}
	}
	return r, nil
}

// AssessObservation compares one externally supplied observation with f.
// It performs no subject execution and returns no causal or acceptance authority.
func AssessObservation(f FrozenObservationSpecification, o ExternalObservation) (Report, error) {
	work, ok := checkedAdd(len(f.wire.Rows), 1)
	if !ok || work > f.wire.Limits.MaxWork {
		return Report{}, Issue{Code: CodeOverflow, Surface: "work", Detail: "assessment work limit exceeded"}
	}
	return assess(f, o)
}

// AssessObservationBatch assesses externally supplied observations within f's limits.
// Each result retains external custody, noncausality, and zero authority.
func AssessObservationBatch(f FrozenObservationSpecification, observations []ExternalObservation) ([]Report, error) {
	if observations == nil {
		return nil, Issue{Code: CodeMissing, Surface: "observations", Detail: "nil batch"}
	}
	if len(observations) > f.wire.Limits.MaxRows {
		return nil, Issue{Code: CodeOverflow, Surface: "observations", Detail: "batch limit exceeded"}
	}
	per, ok := checkedAdd(len(f.wire.Rows), 1)
	if !ok || (len(observations) != 0 && per > f.wire.Limits.MaxWork/len(observations)) {
		return nil, Issue{Code: CodeOverflow, Surface: "work", Detail: "batch assessment work limit exceeded"}
	}
	out := make([]Report, 0, len(observations))
	for _, o := range observations {
		r, e := assess(f, o)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}

// Canonical returns the report's canonical JSON bytes.
func (r Report) Canonical() ([]byte, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	return b, nil
}

// SyntaxCheckedReport records syntax validation only and exposes no semantic report.
type SyntaxCheckedReport struct{ raw []byte }

// ParseReportSyntax validates only the strict canonical JSON grammar. It returns
// no semantic Report and therefore accepts no caller-authored semantic claim.
func ParseReportSyntax(data []byte, max int) (SyntaxCheckedReport, error) {
	if len(data) == 0 || max <= 0 || len(data) > max {
		return SyntaxCheckedReport{}, Issue{Code: CodeOverflow, Surface: "report", Detail: "empty or oversized"}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var r Report
	if e := d.Decode(&r); e != nil {
		return SyntaxCheckedReport{}, Issue{Code: CodeInvalid, Surface: "report", Detail: "strict decode failed"}
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return SyntaxCheckedReport{}, Issue{Code: CodeInvalid, Surface: "report", Detail: "trailing JSON"}
	}
	canon, e := json.Marshal(r)
	if e != nil || !bytes.Equal(canon, data) {
		return SyntaxCheckedReport{}, Issue{Code: CodeInvalid, Surface: "report", Detail: "noncanonical JSON"}
	}
	validClass := r.Classification == SpecInvalid || r.Classification == ObservationMalformed || r.Classification == ObservationMismatch || r.Classification == SuppliedOutcomesMatchQualifiedSpecification || r.Classification == NonDiscriminating || r.Classification == Unqualified
	validBinding := r.ObservationBinding == "CANONICALLY_VERIFIED" || r.ObservationBinding == "UNVERIFIED" || r.ObservationBinding == "MISMATCH"
	if r.Version != Version || !validDigest(r.Identity) || !validDigest(r.SpecificationIdentity) || !validClass || r.ExecutionCustody != "EXTERNAL" || !validBinding || r.CausalExecution != "NOT_OBSERVED" || r.DifferenceWitness != ByteDifferenceWitnessVerified || r.Authority != 0 || r.Accepted || r.Findings == nil {
		return SyntaxCheckedReport{}, Issue{Code: CodeInvalid, Surface: "report", Detail: "invalid report shape or enum"}
	}
	return SyntaxCheckedReport{raw: clone(data)}, nil
}

// VerifyReport is the only report-acceptance path. It recomputes the complete
// report from the frozen specification and supplied raw observation, then
// requires exact canonical equality including report identity and relations.
func VerifyReport(f FrozenObservationSpecification, o ExternalObservation, raw []byte) (Report, error) {
	if _, e := ParseReportSyntax(raw, f.wire.Limits.MaxReportBytes); e != nil {
		return Report{}, e
	}
	want, e := AssessObservation(f, o)
	if e != nil {
		return Report{}, e
	}
	canonical, e := want.Canonical()
	if e != nil || !bytes.Equal(canonical, raw) {
		return Report{}, Issue{Code: CodeInvalid, Surface: "report.binding", Detail: "report does not match recomputed frozen-spec assessment"}
	}
	return want, nil
}
