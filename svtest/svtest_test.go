package svtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jaresty/semantic-vector/vector"
)

type input struct {
	Allowed bool   `json:"allowed"`
	Tag     string `json:"tag"`
}

type codec struct{}

func (codec) Encode(v input) ([]byte, error) { return json.Marshal(v) }
func (codec) Decode(b []byte) (input, error) {
	var v input
	return v, json.Unmarshal(b, &v)
}

type observationCodec struct{}

func (observationCodec) EncodeOutput(v string) ([]byte, error)  { return json.Marshal(v) }
func (observationCodec) EncodeFailure(v string) ([]byte, error) { return json.Marshal(v) }
func (observationCodec) EncodeEffects(v []string) ([]byte, error) {
	if v == nil {
		v = []string{}
	}
	return json.Marshal(v)
}

type adapter struct {
	mu    sync.Mutex
	calls []input
	err   bool
}

func (a *adapter) Execute(ctx context.Context, in input) (vector.Observation[string, string, string], error) {
	a.mu.Lock()
	a.calls = append(a.calls, in)
	a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return vector.Observation[string, string, string]{}, err
	}
	if a.err && !in.Allowed {
		return vector.Observation[string, string, string]{}, errors.New("adapter failed")
	}
	if in.Allowed {
		return vector.Observation[string, string, string]{Output: "ok", Effects: []string{"write"}}, nil
	}
	return vector.Observation[string, string, string]{Failure: "denied", Effects: []string{}}, nil
}

func classify(o vector.Observation[string, string, string]) (string, error) {
	if o.Failure != "" {
		return o.Failure, nil
	}
	return "allowed", nil
}

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func encodeExpectation(t testing.TB, output, failure, class string, effects []string) vector.Expectation[string, string, string] {
	t.Helper()
	oc := observationCodec{}
	o, _ := oc.EncodeOutput(output)
	f, _ := oc.EncodeFailure(failure)
	e, _ := oc.EncodeEffects(effects)
	return vector.Expectation[string, string, string]{
		Output: output, Failure: failure,
		Effects:        vector.Effects[string]{Known: true, Values: append([]string(nil), effects...)},
		Classification: class,
		Canonical:      vector.CanonicalExpectation{Output: o, Failure: f, Effects: e},
	}
}

func fixture(t testing.TB, n int) (*Contract[input, string, string, string], []Row[input, string, string, string], *adapter) {
	t.Helper()
	validBytes, _ := json.Marshal(input{Allowed: true})
	a := &adapter{}
	contract := &Contract[input, string, string, string]{
		Template: vector.BatchTemplate[input, string, string, string]{
			Codec: codec{}, ObservationCodec: observationCodec{}, CodecKey: "input-json/v1",
			ObservationCodecKey: "observation-json/v1", ClosureStateRetained: true,
			AdapterKey: "adapter/v1", Adapter: a, ClassifierKey: "classifier/v1", Classify: classify,
		},
		Version:         vector.VersionV1,
		Oracle:          vector.Oracle{Kind: "reviewed fixture", Source: "svtest_test.go", Revision: "1", Producer: "test", Method: "manual", Review: "reviewed", Independence: vector.Reference, Verification: vector.Verification{Status: vector.VerificationIndependentlyReviewed, Evidence: []vector.ReviewEvidence{{Reviewer: "reviewer", Scope: "fixture", Source: "test", Revision: "1"}}}},
		Valid:           vector.World[input]{Canonical: validBytes},
		ExpectedValid:   encodeExpectation(t, "ok", "", "allowed", []string{"write"}),
		EvidenceCeiling: vector.EvidenceCeiling{Version: "test/v1", CannotEstablish: []string{"domain truth"}},
		Reuse:           vector.ValidObservationReuse{AdapterKey: "adapter/v1", Scope: "this test matrix", Assumptions: []string{"pure for valid world", "replay-safe", "reentrant"}, Status: vector.DeclarationUnverified},
		Limits:          vector.DefaultBatchLimits(),
	}
	rows := make([]Row[input, string, string, string], 0, n)
	for i := n - 1; i >= 0; i-- {
		name := string(rune('a' + i))
		bad := input{Allowed: false, Tag: name}
		badBytes, _ := json.Marshal(bad)
		expected := Expected[string, string, string]{Violating: encodeExpectation(t, "", "denied", "denied", []string{}), Status: vector.Pass, Contrast: vector.ContrastEstablished}
		rows = append(rows, RejectNoEffects(name, vector.Property{ID: name, Statement: "reject " + name, Source: "test", Version: "1"}, name+"/v1", func(input) (input, error) { return bad, nil }, vector.WholeWorldDifference{BeforeDigest: hash(validBytes), AfterDigest: hash(badBytes)}, expected))
	}
	return contract, rows, a
}

func TestRunMatrixRunsOneStableBatchAndPreservesRows(t *testing.T) {
	contract, rows, adapter := fixture(t, 3)
	result, err := RunMatrix(contract, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || result.Full != nil || result.Compact != nil {
		t.Fatalf("diagnostics=%#v full=%v compact=%v", result.Diagnostics, result.Full, result.Compact)
	}
	if len(adapter.calls) != 4 || !adapter.calls[0].Allowed {
		t.Fatalf("adapter calls=%#v", adapter.calls)
	}
	for i, want := range []string{"a", "b", "c"} {
		got := result.Batch.Mutations[i]
		if got.MutationID != want || got.PerturbationKey != want+"/v1" || got.Report.PropertyID != want || adapter.calls[i+1].Tag != want {
			t.Fatalf("row %d got=%#v call=%#v", i, got, adapter.calls[i+1])
		}
	}
}

func TestCheckMatrixUsesRealNamedSubtestsWithoutRerun(t *testing.T) {
	contract, rows, adapter := fixture(t, 2)
	result := CheckMatrix(t, contract, rows)
	if len(result.Diagnostics) != 0 || len(adapter.calls) != 3 {
		t.Fatalf("diagnostics=%#v calls=%d", result.Diagnostics, len(adapter.calls))
	}
}

type executableCalls struct {
	codec, observationCodec, apply, adapter, classifier int
}

type countedCodec struct {
	calls *executableCalls
}

func (c countedCodec) Encode(v input) ([]byte, error) {
	c.calls.codec++
	return codec{}.Encode(v)
}
func (c countedCodec) Decode(b []byte) (input, error) {
	c.calls.codec++
	return codec{}.Decode(b)
}

type countedObservationCodec struct {
	calls *executableCalls
}

func (c countedObservationCodec) EncodeOutput(v string) ([]byte, error) {
	c.calls.observationCodec++
	return observationCodec{}.EncodeOutput(v)
}
func (c countedObservationCodec) EncodeFailure(v string) ([]byte, error) {
	c.calls.observationCodec++
	return observationCodec{}.EncodeFailure(v)
}
func (c countedObservationCodec) EncodeEffects(v []string) ([]byte, error) {
	c.calls.observationCodec++
	return observationCodec{}.EncodeEffects(v)
}

func TestContractDeclarationsFailBeforeAnyExecutableCall(t *testing.T) {
	tests := map[string]func(*Contract[input, string, string, string]){
		"expected-valid-effects-omitted": func(contract *Contract[input, string, string, string]) {
			contract.ExpectedValid.Effects = vector.Effects[string]{}
		},
		"expected-valid-effects-known-but-not-explicit": func(contract *Contract[input, string, string, string]) {
			contract.ExpectedValid.Effects = vector.Effects[string]{Known: true}
		},
		"evidence-ceiling-zero": func(contract *Contract[input, string, string, string]) {
			contract.EvidenceCeiling = vector.EvidenceCeiling{}
		},
		"evidence-ceiling-aliased-empty": func(contract *Contract[input, string, string, string]) {
			contract.EvidenceCeiling.CannotEstablish[0] = ""
		},
		"reuse-declaration-zero": func(contract *Contract[input, string, string, string]) {
			contract.Reuse = vector.ValidObservationReuse{}
		},
		"reuse-assumption-aliased-empty": func(contract *Contract[input, string, string, string]) {
			contract.Reuse.Assumptions[0] = ""
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			contract, rows, adapter := fixture(t, 1)
			calls := &executableCalls{}
			contract.Template.Codec = countedCodec{calls: calls}
			contract.Template.ObservationCodec = countedObservationCodec{calls: calls}
			contract.Template.Classify = func(o vector.Observation[string, string, string]) (string, error) {
				calls.classifier++
				return classify(o)
			}
			originalApply := rows[0].Apply
			rows[0].Apply = func(in input) (input, error) {
				calls.apply++
				return originalApply(in)
			}
			mutate(contract)
			if _, err := RunMatrix(contract, rows); err == nil || !strings.HasPrefix(err.Error(), "svtest: contract") {
				t.Fatalf("err=%v", err)
			}
			calls.adapter = len(adapter.calls)
			if *calls != (executableCalls{}) {
				t.Fatalf("executables called before contract refusal: %#v", calls)
			}
		})
	}
}

func TestDefinitionsRefusedBeforeAdapterExecution(t *testing.T) {
	tests := map[string]func([]Row[input, string, string, string]){
		"duplicate-name": func(rows []Row[input, string, string, string]) {
			rows[1].Name, rows[1].Property.ID = rows[0].Name, rows[0].Name
		},
		"duplicate-key": func(rows []Row[input, string, string, string]) { rows[1].PerturbationKey = rows[0].PerturbationKey },
		"missing-effects": func(rows []Row[input, string, string, string]) {
			rows[0].Expected.Violating.Effects = vector.Effects[string]{}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			contract, rows, adapter := fixture(t, 2)
			mutate(rows)
			if _, err := RunMatrix(contract, rows); err == nil || len(adapter.calls) != 0 {
				t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
			}
		})
	}
}

func TestEqualAndUncontrolledWorldDefinitionsAreRefused(t *testing.T) {
	for _, name := range []string{"equal", "digest-mismatch"} {
		t.Run(name, func(t *testing.T) {
			contract, rows, adapter := fixture(t, 1)
			valid := input{Allowed: true}
			validBytes, _ := json.Marshal(valid)
			rows[0].Apply = func(input) (input, error) { return valid, nil }
			if name == "equal" {
				rows[0].Difference.AfterDigest = hash(validBytes)
			}
			if _, err := RunMatrix(contract, rows); err == nil || len(adapter.calls) != 0 {
				t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
			}
		})
	}
}

func TestConciseDiagnosticsDistinguishFailureClasses(t *testing.T) {
	batch := vector.BatchResult{Mutations: []vector.BatchMutationResult{{MutationID: "row", PerturbationKey: "row/v1", Report: vector.Report{
		PropertyID: "row", Status: vector.Failed, Contrast: vector.ContrastAbsent,
		Valid:          vector.WorldResult{Matched: false},
		Violating:      vector.WorldResult{Classification: "other", Observation: vector.EncodedObservation{Effects: "WyJ3cml0ZSJd"}, Matched: false},
		Classification: vector.ReportClassification{ExpectationMismatch: true, ExecutionFailure: true, TestDefinitionDefect: true},
	}}}}
	diagnostics := compare(batch, map[string]expectedRow{"row": {status: vector.Pass, contrast: vector.ContrastEstablished, class: "denied", effects: []byte("[]")}})
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
	for _, fragment := range []string{"mutation=\"row\"", "property=\"row\"", "key=\"row/v1\"", "status expected=PASS observed=FAILED", "class expected=\"denied\" observed=\"other\"", "effects expected=\"W10\" observed=\"WyJ3cml0ZSJd\"", "contrast expected=ESTABLISHED observed=ABSENT", "test-definition-defect=true", "execution-failure=true", "expectation-mismatch=true"} {
		if !strings.Contains(diagnostics[0].Message, fragment) {
			t.Fatalf("diagnostic %q missing %q", diagnostics[0].Message, fragment)
		}
	}
}

func TestOptionalAuditBytesAreCoreBytes(t *testing.T) {
	contract, rows, _ := fixture(t, 2)
	var full, compact bytes.Buffer
	got, err := RunMatrix(contract, rows, WithFullReport(&full), WithCompactReport(&compact))
	if err != nil {
		t.Fatal(err)
	}
	coreFull, err := got.Batch.Structured()
	if err != nil {
		t.Fatal(err)
	}
	coreCompact, err := got.Batch.Compact()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Full, coreFull) || !bytes.Equal(got.Compact, coreCompact) || !bytes.Equal(full.Bytes(), coreFull) || !bytes.Equal(compact.Bytes(), coreCompact) {
		t.Fatal("svtest audit bytes differ from core")
	}
	if err := vector.VerifyCompactBatch(got.Full, got.Compact); err != nil {
		t.Fatal(err)
	}
}

func TestNilInputsCancellationAndNoEffectsNaming(t *testing.T) {
	contract, rows, _ := fixture(t, 1)
	if _, err := RunMatrix[input, string, string, string](nil, rows); err == nil {
		t.Fatal("nil contract accepted")
	}
	if _, err := RunMatrix(contract, nil); err == nil {
		t.Fatal("nil rows accepted")
	}
	if result := CheckMatrix[input, string, string, string](nil, contract, rows); len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "nil testing.TB") {
		t.Fatalf("nil TB result=%#v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunMatrix(contract, rows, WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation err=%v", err)
	}
	expected := rows[0].Expected
	expected.Violating.Effects = vector.Effects[string]{}
	plain := Reject("x", vector.Property{ID: "x"}, "x/v1", rows[0].Apply, rows[0].Difference, expected)
	if plain.Expected.Violating.Effects.Known {
		t.Fatal("Reject silently defaulted effects")
	}
	none := RejectNoEffects("x", vector.Property{ID: "x"}, "x/v1", rows[0].Apply, rows[0].Difference, expected)
	if !none.Expected.Violating.Effects.Known || none.Expected.Violating.Effects.Values == nil || len(none.Expected.Violating.Effects.Values) != 0 {
		t.Fatalf("RejectNoEffects=%#v", none.Expected.Violating.Effects)
	}
}

func TestPanicPolicyIsInherited(t *testing.T) {
	contract, rows, _ := fixture(t, 1)
	rows[0].Apply = func(input) (input, error) { panic("mutation panic") }
	defer func() {
		if recovered := recover(); recovered != "mutation panic" {
			t.Fatalf("recovered=%#v", recovered)
		}
	}()
	_, _ = RunMatrix(contract, rows)
}
