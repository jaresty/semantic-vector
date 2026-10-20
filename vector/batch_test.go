package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func batchFixture(t *testing.T, n int) (BatchRun[input, string, string, string], *recordingAdapter) {
	t.Helper()
	valid, _ := base(t)
	mutations := make([]BatchMutation[input, string, string, string], 0, n)
	for i := n - 1; i >= 0; i-- {
		v, _ := base(t)
		id := fmt.Sprintf("mutation-%02d", i)
		v.Perturbation.ID = id
		v.Executables.PerturbationKey = id + "/v1"
		v.Perturbation.Apply = func(in input) (input, error) { in.Allowed = false; in.Tags = []string{id}; return in, nil }
		bad := input{Allowed: false, Tags: []string{id}}
		v.Perturbation.Difference.AfterDigest = digest(canon(t, bad))
		mutations = append(mutations, BatchMutation[input, string, string, string]{Vector: v})
	}
	a := &recordingAdapter{}
	return BatchRun[input, string, string, string]{
		Valid: valid, Mutations: mutations,
		Reuse:      ValidObservationReuse{AdapterKey: "adapter/access/v1", Scope: "this frozen batch", Assumptions: []string{"pure for declared valid world", "replay-safe", "reentrant"}, Status: DeclarationUnverified},
		AdapterKey: "adapter/access/v1", Adapter: a, ClassifierKey: "classifier/access/v1", Classify: classifier,
		Limits: DefaultBatchLimits(),
	}, a
}

type recordingAdapter struct{ calls []input }

func (a *recordingAdapter) Execute(_ context.Context, in input) (Observation[string, string, string], error) {
	a.calls = append(a.calls, in)
	if in.Allowed {
		return Observation[string, string, string]{Output: "ok", Effects: []string{"write"}}, nil
	}
	return Observation[string, string, string]{Failure: "denied", Effects: []string{}}, nil
}

func TestRunBatchContrastExecutesValidOnceAndTwentyFourMutationsInStableOrder(t *testing.T) {
	run, adapter := batchFixture(t, 24)
	result, err := RunBatchContrast(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.calls) != 25 || !adapter.calls[0].Allowed {
		t.Fatalf("calls=%d first=%#v", len(adapter.calls), adapter.calls[0])
	}
	if len(result.Mutations) != 24 {
		t.Fatalf("mutations=%d", len(result.Mutations))
	}
	for i, mutation := range result.Mutations {
		want := fmt.Sprintf("mutation-%02d", i)
		if mutation.MutationID != want || !reflect.DeepEqual(mutation.Report.Valid, result.Valid) || mutation.Report.Contrast != ContrastEstablished {
			t.Fatalf("[%d] id=%q contrast=%s valid=%#v shared=%#v", i, mutation.MutationID, mutation.Report.Contrast, mutation.Report.Valid, result.Valid)
		}
		if adapter.calls[i+1].Tags[0] != want {
			t.Fatalf("execution order[%d]=%q", i, adapter.calls[i+1].Tags[0])
		}
		var single Vector[input, string, string, string]
		for _, authored := range run.Mutations {
			if authored.Vector.Perturbation.ID == want {
				single = authored.Vector
				break
			}
		}
		pair, err := RunContrast(context.Background(), ContrastRun[input, string, string, string]{Vector: single, AdapterKey: run.AdapterKey, Adapter: &recordingAdapter{}, ClassifierKey: run.ClassifierKey, Classify: run.Classify, Limits: run.Limits.Pair})
		if err != nil || mutation.Report.Status != pair.Status || mutation.Report.Contrast != pair.Contrast || !reflect.DeepEqual(mutation.Report.Classification, pair.Classification) {
			t.Fatalf("[%d] batch=%#v pair=%#v err=%v", i, mutation.Report.Classification, pair.Classification, err)
		}
	}
	if result.Version != BatchVersionV1 || result.Reuse.Status != DeclarationUnverified || result.Identity == "" {
		t.Fatalf("metadata=%#v", result)
	}
}

func TestBatchAdmissionRejectsMissingReuseAndDuplicateMutationIdentity(t *testing.T) {
	run, adapter := batchFixture(t, 2)
	run.Reuse = ValidObservationReuse{}
	if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
		t.Fatalf("missing reuse: err=%v calls=%d", err, len(adapter.calls))
	}
	run, adapter = batchFixture(t, 2)
	run.Mutations[1] = run.Mutations[0]
	if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
		t.Fatalf("duplicate: err=%v calls=%d", err, len(adapter.calls))
	}
}

func TestCompactBatchReportStrictDeterministicAndMeasured(t *testing.T) {
	run, _ := batchFixture(t, 24)
	result, err := RunBatchContrast(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	full, err := result.Structured()
	if err != nil {
		t.Fatal(err)
	}
	compact, err := result.Compact()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := result.Compact()
	if !bytes.Equal(compact, again) {
		t.Fatal("compact bytes changed")
	}
	parsed, err := ParseCompactBatch(compact)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.BatchIdentity != result.Identity || parsed.FullResultDigest != digest(full) || parsed.OmittedDetailClasses == nil || len(parsed.OmittedDetailClasses) == 0 || parsed.Mutations == nil || len(parsed.Mutations) != 24 {
		t.Fatalf("compact=%#v", parsed)
	}
	var repeated int
	for _, m := range result.Mutations {
		b, e := m.Report.Structured()
		if e != nil {
			t.Fatal(e)
		}
		repeated += len(b)
	}
	var wire map[string]any
	if err := json.Unmarshal(compact, &wire); err != nil {
		t.Fatal(err)
	}
	wire["m"] = nil
	nullMutations, _ := json.Marshal(wire)
	mutations := wire["m"]
	_ = mutations
	for name, bad := range map[string][]byte{
		"trailing":       append(append([]byte{}, compact...), 'x'),
		"unknown":        bytes.Replace(compact, []byte(`{"v"`), []byte(`{"unknown":1,"v"`), 1),
		"duplicate":      bytes.Replace(compact, []byte(`{"v":"semantic-vector-batch/v1"`), []byte(`{"v":"semantic-vector-batch/v1","v":"semantic-vector-batch/v1"`), 1),
		"noncanonical":   append([]byte(" "), compact...),
		"null mutations": nullMutations,
	} {
		if _, err := ParseCompactBatch(bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	t.Logf("compact=%d repeated-full=%d ratio=%.2f%%", len(compact), repeated, 100*float64(len(compact))/float64(repeated))
}

func TestBatchAdmissionLimitsNilMixedAndAliasMutation(t *testing.T) {
	t.Run("nil mutations", func(t *testing.T) {
		run, adapter := batchFixture(t, 1)
		run.Mutations = nil
		if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
		}
	})
	t.Run("mutation limit", func(t *testing.T) {
		run, adapter := batchFixture(t, 2)
		run.Limits.MaxMutations = 1
		if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
		}
	})
	t.Run("byte limit", func(t *testing.T) {
		run, adapter := batchFixture(t, 2)
		run.Limits.MaxTotalCanonicalBytes = 1
		if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
		}
	})
	t.Run("mixed ceiling", func(t *testing.T) {
		run, adapter := batchFixture(t, 2)
		run.Mutations[1].Vector.EvidenceCeiling.Version = "other"
		if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
		}
	})
	t.Run("mixed valid expectation", func(t *testing.T) {
		run, adapter := batchFixture(t, 2)
		run.Mutations[1].Vector.ExpectedValid.Classification = "other"
		if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
		}
	})
	t.Run("declared shared valid expectation", func(t *testing.T) {
		run, adapter := batchFixture(t, 2)
		run.Valid.ExpectedValid.Classification = "other"
		if _, err := RunBatchContrast(context.Background(), run); err == nil || len(adapter.calls) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(adapter.calls))
		}
	})
	t.Run("alias mutation", func(t *testing.T) {
		run, _ := batchFixture(t, 1)
		run.Mutations[0].Vector.Perturbation.Apply = func(in input) (input, error) { in.Tags[0] = "mutated"; in.Allowed = false; return in, nil }
		run.Mutations[0].Vector.Perturbation.Difference.AfterDigest = digest(canon(t, input{Allowed: false, Tags: []string{"mutated"}}))
		result, err := RunBatchContrast(context.Background(), run)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Valid.Matched {
			t.Fatalf("valid corrupted: %#v", result.Valid)
		}
	})
}

type selectiveFailureAdapter struct {
	calls   int
	panicAt int
	failAt  int
}

func (a *selectiveFailureAdapter) Execute(_ context.Context, in input) (Observation[string, string, string], error) {
	a.calls++
	if a.calls == a.panicAt {
		panic("adapter panic")
	}
	if a.calls == a.failAt {
		return Observation[string, string, string]{}, fmt.Errorf("adapter failure")
	}
	if in.Allowed {
		return Observation[string, string, string]{Output: "ok", Effects: []string{"write"}}, nil
	}
	return Observation[string, string, string]{Failure: "denied", Effects: []string{}}, nil
}
func TestBatchPartialFailuresContinueAndAdapterPanicPolicyIsPreserved(t *testing.T) {
	run, _ := batchFixture(t, 3)
	failing := &selectiveFailureAdapter{failAt: 3}
	run.Adapter = failing
	result, err := RunBatchContrast(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if failing.calls != 4 || len(result.Mutations) != 3 || !result.Mutations[1].Report.Classification.ExecutionFailure {
		t.Fatalf("calls=%d result=%#v", failing.calls, result)
	}
	run, _ = batchFixture(t, 1)
	panicking := &selectiveFailureAdapter{panicAt: 2}
	run.Adapter = panicking
	defer func() {
		if recover() == nil {
			t.Fatal("existing adapter panic propagation policy changed")
		}
	}()
	_, _ = RunBatchContrast(context.Background(), run)
}

func BenchmarkRunBatchContrast24(b *testing.B) {
	t := &testing.T{}
	run, _ := batchFixture(t, 24)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := &recordingAdapter{}
		run.Adapter = a
		if _, err := RunBatchContrast(context.Background(), run); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBatchPreservesIndependentPropertiesAndRunContrastParity(t *testing.T) {
	run, _ := batchFixture(t, 2)
	for i := range run.Mutations {
		run.Mutations[i].Vector.Property = Property{ID: fmt.Sprintf("property-%d", i), Statement: fmt.Sprintf("statement %d", i), Source: fmt.Sprintf("source/%d", i), Version: "1"}
	}
	result, err := RunBatchContrast(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range result.Mutations {
		var authored Vector[input, string, string, string]
		for _, candidate := range run.Mutations {
			if candidate.Vector.Perturbation.ID == m.MutationID {
				authored = candidate.Vector
				break
			}
		}
		pair, err := RunContrast(context.Background(), ContrastRun[input, string, string, string]{Vector: authored, AdapterKey: run.AdapterKey, Adapter: &recordingAdapter{}, ClassifierKey: run.ClassifierKey, Classify: run.Classify, Limits: run.Limits.Pair})
		if err != nil {
			t.Fatal(err)
		}
		if m.Report.PropertyID != authored.Property.ID || !reflect.DeepEqual(m.Report.dto(), pair.dto()) {
			t.Fatalf("[%d] batch=%#v pair=%#v", i, m.Report, pair)
		}
	}
}

func TestBatchSortsAndRejectsDuplicatesBeforeExecutableClosures(t *testing.T) {
	run, _ := batchFixture(t, 3)
	order := []string{}
	for i := range run.Mutations {
		id := run.Mutations[i].Vector.Perturbation.ID
		run.Mutations[i].Vector.Perturbation.Apply = func(in input) (input, error) {
			order = append(order, id)
			in.Allowed = false
			in.Tags = []string{id}
			return in, nil
		}
	}
	if _, err := RunBatchContrast(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"mutation-00", "mutation-01", "mutation-02"}) {
		t.Fatalf("apply order=%v", order)
	}
	run, _ = batchFixture(t, 2)
	calls := 0
	for i := range run.Mutations {
		run.Mutations[i].Vector.Perturbation.Apply = func(in input) (input, error) { calls++; return in, nil }
	}
	run.Mutations[1].Vector.Perturbation.ID = run.Mutations[0].Vector.Perturbation.ID
	if _, err := RunBatchContrast(context.Background(), run); err == nil || calls != 0 {
		t.Fatalf("err=%v closure calls=%d", err, calls)
	}
}

func TestCompactSemanticValidationAndRelationalVerification(t *testing.T) {
	run, _ := batchFixture(t, 2)
	result, err := RunBatchContrast(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := result.Structured()
	compact, _ := result.Compact()
	if err := VerifyCompactBatch(full, compact); err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(compact, &wire); err != nil {
		t.Fatal(err)
	}
	mutate := func(key string, value any) []byte {
		copy := map[string]any{}
		for k, v := range wire {
			copy[k] = v
		}
		copy[key] = value
		b, _ := json.Marshal(copy)
		return b
	}
	for name, bad := range map[string][]byte{"reuse": mutate("r", map[string]any{"adapter_key": "adapter/access/v1", "scope": "x", "assumptions": []any{"x"}, "status": "VERIFIED"}), "batch digest": mutate("i", "sha256:xyz"), "verification": mutate("p", "FABRICATED"), "limits": mutate("l", map[string]any{}), "omissions": mutate("o", []any{})} {
		if _, err := ParseCompactBatch(bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	var canonical compactWire
	if err := json.Unmarshal(compact, &canonical); err != nil {
		t.Fatal(err)
	}
	wireMutation := func(change func(*compactWire)) []byte {
		var w compactWire
		b, _ := json.Marshal(canonical)
		_ = json.Unmarshal(b, &w)
		change(&w)
		out, _ := json.Marshal(w)
		return out
	}
	for name, bad := range map[string][]byte{
		"status":          wireMutation(func(w *compactWire) { w.M[0][3] = "FABRICATED" }),
		"contrast":        wireMutation(func(w *compactWire) { w.M[0][4] = "FABRICATED" }),
		"discrimination":  wireMutation(func(w *compactWire) { w.M[0][6] = "ABSENT" }),
		"attribution":     wireMutation(func(w *compactWire) { w.M[0][7] = "IMPLEMENTATION_DEFECT" }),
		"mutation digest": wireMutation(func(w *compactWire) { w.M[0][2] = "sha256:not-a-digest" }),
		"effects digest":  wireMutation(func(w *compactWire) { w.D[0] = "sha256:not-a-digest" }),
		"ceiling":         wireMutation(func(w *compactWire) { w.C = EvidenceCeiling{} }),
		"zero limit":      wireMutation(func(w *compactWire) { w.L.MaxWork = 0 }),
	} {
		if _, err := ParseCompactBatch(bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	other, _ := batchFixture(t, 1)
	otherResult, _ := RunBatchContrast(context.Background(), other)
	otherFull, _ := otherResult.Structured()
	if err := VerifyCompactBatch(otherFull, compact); err == nil {
		t.Fatal("foreign full result verified")
	}
}

func TestBatchCanonicalSnapshotExactLimit(t *testing.T) {
	run, _ := batchFixture(t, 2)
	size, err := BatchCanonicalSnapshotSize(run)
	if err != nil {
		t.Fatal(err)
	}
	run.Limits.MaxTotalCanonicalBytes = size
	if _, err := RunBatchContrast(context.Background(), run); err != nil {
		t.Fatalf("exact bound rejected: %v", err)
	}
	run.Limits.MaxTotalCanonicalBytes = size - 1
	if _, err := RunBatchContrast(context.Background(), run); err == nil {
		t.Fatal("bound-1 accepted")
	}
}

func batchPlanFixture(t *testing.T, n int) (BatchPlan[input, string, string, string], *recordingAdapter) {
	t.Helper()
	run, adapter := batchFixture(t, n)
	rows := make([]BatchRow[input, string, string, string], 0, n)
	for _, mutation := range run.Mutations {
		v := mutation.Vector
		rows = append(rows, BatchRow[input, string, string, string]{Property: v.Property, Mutation: v.Perturbation, PerturbationKey: v.Executables.PerturbationKey, ExpectedViolating: v.ExpectedViolating})
	}
	return BatchPlan[input, string, string, string]{
		Template: BatchTemplate[input, string, string, string]{Codec: run.Valid.Codec, ObservationCodec: run.Valid.ObservationCodec, CodecKey: run.Valid.Executables.CodecKey, ObservationCodecKey: run.Valid.Executables.ObservationCodecKey, ClosureStateRetained: run.Valid.Executables.ClosureStateRetained, AdapterKey: run.AdapterKey, Adapter: adapter, ClassifierKey: run.ClassifierKey, Classify: run.Classify},
		Version:  run.Valid.Version, Oracle: run.Valid.Oracle, Valid: run.Valid.Valid, ExpectedValid: run.Valid.ExpectedValid, EvidenceCeiling: run.Valid.EvidenceCeiling,
		Rows: rows, Reuse: run.Reuse, Limits: run.Limits,
	}, adapter
}

func TestBatchPlanPreservesDistinctRowKeysAcrossPermutation(t *testing.T) {
	plan, _ := batchPlanFixture(t, 3)
	plan.Rows[0], plan.Rows[2] = plan.Rows[2], plan.Rows[0]
	result, err := RunBatchPlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	for i, mutation := range result.Mutations {
		wantID := fmt.Sprintf("mutation-%02d", i)
		wantKey := wantID + "/v1"
		if mutation.MutationID != wantID || mutation.PerturbationKey != wantKey {
			t.Fatalf("row %d: id=%q key=%q", i, mutation.MutationID, mutation.PerturbationKey)
		}
	}
}

func TestBatchPlanRejectsEmptyDuplicateAndInconsistentRowKeysBeforeClosures(t *testing.T) {
	for name, mutate := range map[string]func(*BatchPlan[input, string, string, string]){
		"empty": func(plan *BatchPlan[input, string, string, string]) { plan.Rows[0].PerturbationKey = "" },
		"duplicate": func(plan *BatchPlan[input, string, string, string]) {
			plan.Rows[1].PerturbationKey = plan.Rows[0].PerturbationKey
		},
		"inconsistent": func(plan *BatchPlan[input, string, string, string]) {
			plan.Rows[1].Mutation.ID = plan.Rows[0].Mutation.ID
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, _ := batchPlanFixture(t, 2)
			calls := 0
			for i := range plan.Rows {
				plan.Rows[i].Mutation.Apply = func(in input) (input, error) { calls++; return in, nil }
			}
			mutate(&plan)
			if _, err := RunBatchPlan(context.Background(), plan); err == nil || calls != 0 {
				t.Fatalf("err=%v closure calls=%d", err, calls)
			}
		})
	}
}

func TestBatchIdentityIsPermutationStableAndDomainSeparated(t *testing.T) {
	a, _ := batchFixture(t, 3)
	b, _ := batchFixture(t, 3)
	b.Mutations[0], b.Mutations[2] = b.Mutations[2], b.Mutations[0]
	ra, err := RunBatchContrast(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := RunBatchContrast(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if ra.Identity != rb.Identity {
		t.Fatalf("permutation changed identity: %s != %s", ra.Identity, rb.Identity)
	}
	fullA, _ := ra.Structured()
	fullB, _ := rb.Structured()
	compactA, _ := ra.Compact()
	compactB, _ := rb.Compact()
	if !bytes.Equal(fullA, fullB) || !bytes.Equal(compactA, compactB) {
		t.Fatal("permutation changed canonical full or compact bytes")
	}
	pairID, err := (Runner[input, string, string, string]{Limits: DefaultLimits()}).Identity(a.Valid)
	if err != nil {
		t.Fatal(err)
	}
	if ra.Identity == pairID {
		t.Fatal("batch identity collided with pair identity")
	}
}
