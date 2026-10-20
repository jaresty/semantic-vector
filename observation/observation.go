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

const Version = "semantic-vector-observation/v1"

var ErrInvalid = errors.New("invalid observation specification")

type Code string

const (
	CodeMissing    Code = "MISSING"
	CodeInvalid    Code = "INVALID"
	CodeDuplicate  Code = "DUPLICATE_IDENTITY"
	CodeUnstable   Code = "UNSTABLE_CODEC"
	CodeCodecPanic Code = "CODEC_PANIC"
	CodeOverflow   Code = "OVERFLOW"
	CodeMismatch   Code = "OBSERVATION_MISMATCH"
)

type Issue struct {
	Code            Code `json:"code"`
	Surface, Detail string
}

func (i Issue) Error() string { return fmt.Sprintf("%s surface=%q: %s", i.Code, i.Surface, i.Detail) }
func (i Issue) Unwrap() error { return ErrInvalid }

type Limits struct{ MaxCanonicalBytes, MaxReportBytes, MaxRows, MaxWork int }

func DefaultLimits() Limits { return Limits{64 << 10, 1 << 20, 1024, 10000} }

type Codec[I any] interface{ Encode(I) ([]byte, error) }

type Property struct{ ID, Statement, Source, Version string }
type ByteSpan struct {
	BeforeStart, BeforeEnd int
	AfterStart, AfterEnd   int
}
type Difference struct {
	PropertyID                string
	BeforeDigest, AfterDigest string
	Claims                    []string
	Spans                     []ByteSpan
	PropertyStatus            string
}

const (
	PropertyDeclaredUnverified    = "DECLARED_UNVERIFIED"
	ByteDifferenceWitnessVerified = "BYTE_DIFFERENCE_WITNESS_VERIFIED"
)

type Outcome struct{ Output, FailureClass, Effects []byte }
type Expected struct{ Valid, Violating Outcome }
type ReviewEvidence struct{ Reviewer, Scope, Source, Revision string }
type Oracle struct {
	Kind, Source, Revision, Producer, Method string
	Independence                             string
	Verification                             string
	Evidence                                 []ReviewEvidence
}
type EvidenceCeiling struct {
	Version         string
	CannotEstablish []string
}
type MutationIdentity struct {
	ID, PropertyID string
	Before, After  []byte
}
type Row[I any] struct {
	ID         string
	Property   Property
	Violating  I
	Difference Difference
	Expected   Expected
	Mutation   *MutationIdentity
}

type ObservationSpecification[I any] struct {
	Version         string
	CodecKey        string
	Codec           Codec[I]
	Valid           I
	Rows            []Row[I]
	Oracle          Oracle
	EvidenceCeiling EvidenceCeiling
	Limits          Limits
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
type FrozenObservationSpecification struct {
	wire      frozenWire
	canonical []byte
	identity  string
}

func (f FrozenObservationSpecification) Identity() string { return f.identity }
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

type ObservedOutcome struct {
	Output, FailureClass, Effects []byte `json:",omitempty"`
}
type ObservedRow struct {
	ID                               string `json:"id"`
	ViolatingInput                   []byte `json:"violating_input"`
	Valid, Violating                 ObservedOutcome
	ReceiptDigest, ReceiptProvenance string
}
type ExternalObservation struct {
	Version, SpecificationIdentity string
	ValidInput                     []byte
	Rows                           []ObservedRow
}

type Classification string

const (
	SpecInvalid                                 Classification = "SPEC_INVALID"
	ObservationMalformed                        Classification = "OBSERVATION_MALFORMED"
	ObservationMismatch                         Classification = "OBSERVATION_MISMATCH"
	SuppliedOutcomesMatchQualifiedSpecification Classification = "SUPPLIED_OUTCOMES_MATCH_QUALIFIED_SPECIFICATION"
	NonDiscriminating                           Classification = "NON_DISCRIMINATING"
	Unqualified                                 Classification = "UNQUALIFIED"
)

type Report struct {
	Version, Identity, SpecificationIdentity              string
	Classification                                        Classification
	ExecutionCustody, ObservationBinding, CausalExecution string
	DifferenceWitness                                     string
	Authority                                             int
	Accepted                                              bool
	OracleStatus                                          string
	Findings                                              []Issue
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
func AssessObservation(f FrozenObservationSpecification, o ExternalObservation) (Report, error) {
	work, ok := checkedAdd(len(f.wire.Rows), 1)
	if !ok || work > f.wire.Limits.MaxWork {
		return Report{}, Issue{Code: CodeOverflow, Surface: "work", Detail: "assessment work limit exceeded"}
	}
	return assess(f, o)
}
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
func (r Report) Canonical() ([]byte, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	return b, nil
}

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
