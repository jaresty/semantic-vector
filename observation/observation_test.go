package observation

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type input struct {
	Allowed bool   `json:"allowed"`
	Tag     string `json:"tag"`
}
type codec struct {
	unstable, panic, fail bool
	n                     int
}

func (c *codec) Encode(v input) ([]byte, error) {
	if c.panic {
		panic("codec")
	}
	if c.fail {
		return nil, errors.New("codec failure")
	}
	c.n++
	b, _ := json.Marshal(v)
	if c.unstable && c.n%2 == 0 {
		return append(b, ' '), nil
	}
	return b, nil
}
func h(b []byte) string { return digest(b) }
func witness(before, after []byte) []ByteSpan {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	bi, ai := len(before), len(after)
	for bi > prefix && ai > prefix && before[bi-1] == after[ai-1] {
		bi--
		ai--
	}
	return []ByteSpan{{BeforeStart: prefix, BeforeEnd: bi, AfterStart: prefix, AfterEnd: ai}}
}
func good(t *testing.T) (ObservationSpecification[input], ExternalObservation) {
	t.Helper()
	c := &codec{}
	vb, _ := c.Encode(input{Allowed: true})
	xb, _ := c.Encode(input{Allowed: false, Tag: "x"})
	valid := Outcome{[]byte(`"ok"`), []byte(`""`), []byte(`["write"]`)}
	bad := Outcome{[]byte(`""`), []byte(`"denied"`), []byte(`[]`)}
	s := ObservationSpecification[input]{Version: Version, CodecKey: "json/input/v1", Codec: &codec{}, Valid: input{Allowed: true}, Rows: []Row[input]{{ID: "deny-x", Property: Property{"deny-x", "deny x", "test", "1"}, Violating: input{Allowed: false, Tag: "x"}, Difference: Difference{PropertyID: "deny-x", BeforeDigest: h(vb), AfterDigest: h(xb), Claims: []string{"allowed changes true to false"}, Spans: witness(vb, xb), PropertyStatus: PropertyDeclaredUnverified}, Expected: Expected{valid, bad}, Mutation: &MutationIdentity{"deny-x/v1", "deny-x", vb, xb}}}, Oracle: Oracle{"reference", "fixture", "1", "test", "handwritten", "independent", "INDEPENDENTLY_REVIEWED", []ReviewEvidence{{"reviewer", "row", "fixture", "1"}}}, EvidenceCeiling: EvidenceCeiling{"1", []string{"runtime causality", "production behavior"}}, Limits: DefaultLimits()}
	f, e := FreezeObservationSpecification(s)
	if e != nil {
		t.Fatal(e)
	}
	o := ExternalObservation{Version: Version, SpecificationIdentity: f.Identity(), ValidInput: clone(vb), Rows: []ObservedRow{{ID: "deny-x", ViolatingInput: clone(xb), Valid: ObservedOutcome{valid.Output, valid.FailureClass, valid.Effects}, Violating: ObservedOutcome{bad.Output, bad.FailureClass, bad.Effects}, ReceiptDigest: h([]byte("receipt")), ReceiptProvenance: "go test"}}}
	return s, o
}

func TestFreezeAndAssessExternalCustodyDeterministic(t *testing.T) {
	s, o := good(t)
	f, e := FreezeObservationSpecification(s)
	if e != nil {
		t.Fatal(e)
	}
	before := f.Canonical()
	a, e := AssessObservation(f, o)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := AssessObservation(f, o)
	if a.Identity != b.Identity || a.Classification != SuppliedOutcomesMatchQualifiedSpecification || a.ExecutionCustody != "EXTERNAL" || a.ObservationBinding != "CANONICALLY_VERIFIED" || a.CausalExecution != "NOT_OBSERVED" || a.Authority != 0 || a.Accepted {
		t.Fatalf("report=%#v again=%#v", a, b)
	}
	raw, _ := a.Canonical()
	t.Logf("EVIDENCE bytes=%d digest=%s", len(raw), digest(raw))
	parsed, e := VerifyReport(f, o, raw)
	if e != nil || parsed.Identity != a.Identity {
		t.Fatalf("verify=%#v %v", parsed, e)
	}
	s.Rows[0].Difference.Claims[0] = "mutated"
	s.Rows[0].Difference.Spans[0].BeforeStart++
	s.Rows[0].Expected.Valid.Output[0] = 'x'
	s.Rows[0].Expected.Violating.Effects = nil
	s.Rows[0].Mutation.Before[0] = 'x'
	s.Oracle.Evidence[0].Scope = "mutated"
	s.EvidenceCeiling.CannotEstablish[0] = "mutated"
	if !bytes.Equal(before, f.Canonical()) || f.wire.Rows[0].Expected.Violating.Effects == nil {
		t.Fatal("alias changed frozen spec or empty non-nil was lost")
	}
}

func TestFreezeRejectsBadDefinitionsInTypedOrder(t *testing.T) {
	base, _ := good(t)
	tests := []struct {
		name    string
		mutate  func(*ObservationSpecification[input])
		code    Code
		surface string
	}{
		{"version", func(s *ObservationSpecification[input]) { s.Version = "" }, CodeInvalid, "version"},
		{"limits", func(s *ObservationSpecification[input]) { s.Limits.MaxWork = 0 }, CodeInvalid, "limits"},
		{"unstable codec", func(s *ObservationSpecification[input]) { s.Codec = &codec{unstable: true} }, CodeUnstable, "codec"},
		{"codec error", func(s *ObservationSpecification[input]) { s.Codec = &codec{fail: true} }, CodeInvalid, "codec"},
		{"identical worlds", func(s *ObservationSpecification[input]) {
			s.Rows[0].Violating = s.Valid
			s.Rows[0].Difference.AfterDigest = s.Rows[0].Difference.BeforeDigest
		}, CodeInvalid, "rows[0].worlds"},
		{"unclaimed difference", func(s *ObservationSpecification[input]) { s.Rows[0].Difference.Claims = nil }, CodeInvalid, "rows[0].difference"},
		{"multi difference", func(s *ObservationSpecification[input]) { s.Rows[0].Difference.Claims = []string{"allowed", "tag"} }, CodeInvalid, "rows[0].difference"},
		{"omitted effects", func(s *ObservationSpecification[input]) { s.Rows[0].Expected.Violating.Effects = nil }, CodeMissing, "rows[0].expected"},
		{"duplicate IDs", func(s *ObservationSpecification[input]) { s.Rows = append(s.Rows, s.Rows[0]) }, CodeDuplicate, "rows[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			s.Rows = append([]Row[input](nil), base.Rows...)
			tc.mutate(&s)
			_, e := FreezeObservationSpecification(s)
			var issue Issue
			if !errors.As(e, &issue) || issue.Code != tc.code || issue.Surface != tc.surface {
				t.Fatalf("err=%#v want %s %s", e, tc.code, tc.surface)
			}
		})
	}
}

func TestDefectPrecedenceAndAssessmentWorkOverflow(t *testing.T) {
	s, o := good(t)
	s.Version = "wrong"
	s.Codec = &codec{panic: true}
	if _, err := FreezeObservationSpecification(s); err == nil || !strings.Contains(err.Error(), `surface="version"`) {
		t.Fatalf("precedence=%v", err)
	}
	s, o = good(t)
	f, _ := FreezeObservationSpecification(s)
	f.wire.Limits.MaxWork = len(f.wire.Rows)
	if _, err := AssessObservation(f, o); err == nil {
		t.Fatal("assessment work overflow accepted")
	}
	f.wire.Limits.MaxWork = 1
	if _, err := AssessObservationBatch(f, []ExternalObservation{o, o}); err == nil {
		t.Fatal("batch work overflow accepted")
	}
}

func TestCodecPanicContained(t *testing.T) {
	s, _ := good(t)
	s.Codec = &codec{panic: true}
	_, e := FreezeObservationSpecification(s)
	var issue Issue
	if !errors.As(e, &issue) || issue.Code != CodeCodecPanic {
		t.Fatalf("err=%#v", e)
	}
}
func TestObservationClassifications(t *testing.T) {
	s, o := good(t)
	f, _ := FreezeObservationSpecification(s)
	cases := []struct {
		name   string
		mutate func(*ExternalObservation)
		want   Classification
	}{
		{"malformed", func(o *ExternalObservation) { o.Rows[0].Valid.Effects = nil }, ObservationMalformed},
		{"binding mismatch", func(o *ExternalObservation) { o.Rows[0].ViolatingInput = []byte(`{}`) }, ObservationMismatch},
		{"outcome mismatch", func(o *ExternalObservation) { o.Rows[0].Violating.FailureClass = []byte(`"other"`) }, ObservationMismatch},
		{"constant", func(o *ExternalObservation) { o.Rows[0].Violating = o.Rows[0].Valid }, NonDiscriminating},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := o
			x.Rows = append([]ObservedRow(nil), o.Rows...)
			tc.mutate(&x)
			r, e := AssessObservation(f, x)
			if e != nil || r.Classification != tc.want {
				t.Fatalf("r=%#v e=%v", r, e)
			}
		})
	}
	s.Oracle.Verification = "UNVERIFIED"
	s.Oracle.Evidence = nil
	f, _ = FreezeObservationSpecification(s)
	o.SpecificationIdentity = f.Identity()
	r, _ := AssessObservation(f, o)
	if r.Classification != Unqualified || r.OracleStatus != "UNVERIFIED" {
		t.Fatalf("r=%#v", r)
	}
}
func TestDifferenceWitnessRejectsAdversarialSpans(t *testing.T) {
	base, _ := good(t)
	cases := map[string]func(*ObservationSpecification[input]){
		"extra unclaimed byte": func(s *ObservationSpecification[input]) { s.Rows[0].Difference.Spans[0].AfterEnd-- },
		"overlap": func(s *ObservationSpecification[input]) {
			x := s.Rows[0].Difference.Spans[0]
			s.Rows[0].Difference.Spans = []ByteSpan{x, x}
		},
		"order": func(s *ObservationSpecification[input]) {
			x := s.Rows[0].Difference.Spans[0]
			s.Rows[0].Difference.Spans = []ByteSpan{x, {BeforeStart: 0, BeforeEnd: 1, AfterStart: 0, AfterEnd: 1}}
		},
		"bounds": func(s *ObservationSpecification[input]) { s.Rows[0].Difference.Spans[0].AfterEnd = 1 << 20 },
		"identical": func(s *ObservationSpecification[input]) {
			s.Rows[0].Violating = s.Valid
			s.Rows[0].Difference.AfterDigest = s.Rows[0].Difference.BeforeDigest
		},
		"missing status": func(s *ObservationSpecification[input]) { s.Rows[0].Difference.PropertyStatus = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := base
			s.Rows = append([]Row[input](nil), base.Rows...)
			s.Rows[0].Difference.Spans = append([]ByteSpan(nil), base.Rows[0].Difference.Spans...)
			mutate(&s)
			if _, err := FreezeObservationSpecification(s); err == nil {
				t.Fatal("bad witness accepted")
			}
		})
	}
	if err := verifyByteDifference([]byte("aXXbYYc"), []byte("a11b22c"), Difference{PropertyStatus: PropertyDeclaredUnverified, Spans: []ByteSpan{{1, 3, 1, 3}, {4, 6, 4, 6}}}); err != nil {
		t.Fatalf("multi-span rejected: %v", err)
	}
}

func TestReportSyntaxNeverAcceptsSemanticsAndBoundVerifyRejectsForgery(t *testing.T) {
	s, o := good(t)
	f, _ := FreezeObservationSpecification(s)
	r, _ := AssessObservation(f, o)
	raw, _ := r.Canonical()
	if _, err := ParseReportSyntax(raw, len(raw)); err != nil {
		t.Fatal(err)
	}
	for name, forge := range map[string]func(*Report){
		"classification":  func(x *Report) { x.Classification = ObservationMismatch },
		"binding":         func(x *Report) { x.ObservationBinding = "FORGED" },
		"oracle":          func(x *Report) { x.OracleStatus = "FORGED" },
		"spec identity":   func(x *Report) { x.SpecificationIdentity = h([]byte("other")) },
		"report identity": func(x *Report) { x.Identity = h([]byte("other")) },
	} {
		t.Run(name, func(t *testing.T) {
			x := r
			forge(&x)
			b, _ := json.Marshal(x)
			if _, err := VerifyReport(f, o, b); err == nil {
				t.Fatal("forgery verified")
			}
		})
	}
	badEnum := r
	badEnum.Classification = Classification("FORGED")
	badEnumBytes, _ := json.Marshal(badEnum)
	if _, err := ParseReportSyntax(badEnumBytes, len(badEnumBytes)); err == nil {
		t.Fatal("unknown enum accepted")
	}
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	object["unknown"] = true
	unknown, _ := json.Marshal(object)
	for name, bad := range map[string][]byte{"unknown": unknown, "duplicate": bytes.Replace(raw, []byte(`{"Version"`), []byte(`{"Version":"semantic-vector-observation/v1","Version"`), 1), "trailing": append(clone(raw), 'x'), "noncanonical": append([]byte(" "), raw...)} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReportSyntax(bad, len(bad)+1); err == nil {
				t.Fatal("parser attack accepted")
			}
		})
	}
}

func TestStrictCanonicalReportAndBatch(t *testing.T) {
	s, o := good(t)
	f, _ := FreezeObservationSpecification(s)
	r, _ := AssessObservation(f, o)
	raw, _ := r.Canonical()
	if _, e := ParseReportSyntax(append(raw, '\n'), DefaultLimits().MaxReportBytes); e == nil {
		t.Fatal("noncanonical report accepted")
	}
	got, e := AssessObservationBatch(f, []ExternalObservation{o, o})
	if e != nil || len(got) != 2 || got[0].Identity != got[1].Identity {
		t.Fatalf("batch=%#v err=%v", got, e)
	}
	if x, e := AssessObservationBatch(f, nil); e == nil || x != nil {
		t.Fatal("nil batch accepted")
	}
}
