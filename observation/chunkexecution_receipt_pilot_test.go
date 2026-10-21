package observation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jaresty/semantic-vector/observation"
)

const (
	chunkExecutionSourceRevision = "597dcd1bef1b161c3f8fb3083cc57a51436b52e6"
	chunkExecutionSourcePath     = "/private/tmp/adr0011-v8-local-main-integration/internal/censuschunking/execution_test.go"
	chunkExecutionSourceTest     = "TestChunkExecutionReceiptV1StrictCanonicalIdentity"
)

type pilotCounters struct {
	Parser    int `json:"parser_calls"`
	Validator int `json:"validator_calls"`
	Mutation  int `json:"mutation_calls"`
}

type chunkExecutionInput struct {
	Authority int `json:"authority"`
}

type chunkExecutionCodec struct{}

func (chunkExecutionCodec) Encode(input chunkExecutionInput) ([]byte, error) {
	return json.Marshal(input)
}

type producedChunkExecution struct {
	validReceipt     []byte
	violatingReceipt []byte
	validOutcome     observation.Outcome
	violatingOutcome observation.Outcome
	subject          pilotCounters
	produced         bool
}

type capturedChunkExecution struct {
	validReceipt     []byte
	violatingReceipt []byte
	validOutcome     observation.ObservedOutcome
	violatingOutcome observation.ObservedOutcome
	subject          pilotCounters
	receipt          pilotCounters
}

func produceStrictChunkExecution(expected pilotCounters) (producedChunkExecution, error) {
	produced := producedChunkExecution{
		validReceipt:     []byte(`{"authority":0}`),
		violatingReceipt: []byte(`{"authority":1}`),
		validOutcome: observation.Outcome{
			Output:       []byte(`{"strict_identity":"accepted"}`),
			FailureClass: []byte(`""`),
			Effects:      []byte(`[]`),
		},
		violatingOutcome: observation.Outcome{
			Output:       []byte(`{"strict_identity":"rejected"}`),
			FailureClass: []byte(`"authority_changed"`),
			Effects:      []byte(`[]`),
		},
		subject:  pilotCounters{Parser: 6, Validator: 20, Mutation: 19},
		produced: true,
	}
	if produced.subject != expected {
		return producedChunkExecution{}, fmt.Errorf("strict subject counters: got %+v want %+v", produced.subject, expected)
	}
	return produced, nil
}

func captureProducedChunkExecution(produced producedChunkExecution) (capturedChunkExecution, error) {
	if !produced.produced {
		return capturedChunkExecution{}, fmt.Errorf("capture before subject production")
	}
	return capturedChunkExecution{
		validReceipt:     bytes.Clone(produced.validReceipt),
		violatingReceipt: bytes.Clone(produced.violatingReceipt),
		validOutcome: observation.ObservedOutcome{
			Output:       bytes.Clone(produced.validOutcome.Output),
			FailureClass: bytes.Clone(produced.validOutcome.FailureClass),
			Effects:      bytes.Clone(produced.validOutcome.Effects),
		},
		violatingOutcome: observation.ObservedOutcome{
			Output:       bytes.Clone(produced.violatingOutcome.Output),
			FailureClass: bytes.Clone(produced.violatingOutcome.FailureClass),
			Effects:      bytes.Clone(produced.violatingOutcome.Effects),
		},
		subject: produced.subject,
		receipt: pilotCounters{},
	}, nil
}

func cloneCapturedChunkExecution(captured capturedChunkExecution) capturedChunkExecution {
	return capturedChunkExecution{
		validReceipt:     bytes.Clone(captured.validReceipt),
		violatingReceipt: bytes.Clone(captured.violatingReceipt),
		validOutcome: observation.ObservedOutcome{
			Output:       bytes.Clone(captured.validOutcome.Output),
			FailureClass: bytes.Clone(captured.validOutcome.FailureClass),
			Effects:      bytes.Clone(captured.validOutcome.Effects),
		},
		violatingOutcome: observation.ObservedOutcome{
			Output:       bytes.Clone(captured.violatingOutcome.Output),
			FailureClass: bytes.Clone(captured.violatingOutcome.FailureClass),
			Effects:      bytes.Clone(captured.violatingOutcome.Effects),
		},
		subject: captured.subject,
		receipt: captured.receipt,
	}
}

func mutateOriginalProducedSlices(produced *producedChunkExecution) {
	for _, data := range [][]byte{
		produced.validReceipt,
		produced.violatingReceipt,
		produced.validOutcome.Output,
		produced.validOutcome.FailureClass,
		produced.validOutcome.Effects,
		produced.violatingOutcome.Output,
		produced.violatingOutcome.FailureClass,
		produced.violatingOutcome.Effects,
	} {
		data[0] ^= 1
	}
}

func equalCapturedChunkExecution(a, b capturedChunkExecution) bool {
	return bytes.Equal(a.validReceipt, b.validReceipt) &&
		bytes.Equal(a.violatingReceipt, b.violatingReceipt) &&
		bytes.Equal(a.validOutcome.Output, b.validOutcome.Output) &&
		bytes.Equal(a.validOutcome.FailureClass, b.validOutcome.FailureClass) &&
		bytes.Equal(a.validOutcome.Effects, b.validOutcome.Effects) &&
		bytes.Equal(a.violatingOutcome.Output, b.violatingOutcome.Output) &&
		bytes.Equal(a.violatingOutcome.FailureClass, b.violatingOutcome.FailureClass) &&
		bytes.Equal(a.violatingOutcome.Effects, b.violatingOutcome.Effects) &&
		a.subject == b.subject && a.receipt == b.receipt
}

func pilotDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func freezeChunkExecutionPilot(t testing.TB, captured capturedChunkExecution) (observation.FrozenObservationSpecification, observation.ExternalObservation) {
	t.Helper()
	valid := chunkExecutionInput{Authority: 0}
	violating := chunkExecutionInput{Authority: 1}
	frozen, err := observation.FreezeObservationSpecification(observation.ObservationSpecification[chunkExecutionInput]{
		Version:  observation.Version,
		CodecKey: "json/lsp-trace-chunkexecution-receipt-pilot/v1",
		Codec:    chunkExecutionCodec{},
		Valid:    valid,
		Rows: []observation.Row[chunkExecutionInput]{{
			ID: "strict-canonical-identity",
			Property: observation.Property{
				ID:        "strict-canonical-identity",
				Statement: "the copied strict ChunkExecution receipt uses the expected authority byte",
				Source:    chunkExecutionSourcePath + "#" + chunkExecutionSourceTest,
				Version:   chunkExecutionSourceRevision,
			},
			Violating: violating,
			Difference: observation.Difference{
				PropertyID:     "strict-canonical-identity",
				BeforeDigest:   pilotDigest(captured.validReceipt),
				AfterDigest:    pilotDigest(captured.violatingReceipt),
				Claims:         []string{"authority byte changes from 0 to 1"},
				Spans:          []observation.ByteSpan{{BeforeStart: 13, BeforeEnd: 14, AfterStart: 13, AfterEnd: 14}},
				PropertyStatus: observation.PropertyDeclaredUnverified,
			},
			Expected: observation.Expected{
				Valid: observation.Outcome{
					Output:       bytes.Clone(captured.validOutcome.Output),
					FailureClass: bytes.Clone(captured.validOutcome.FailureClass),
					Effects:      bytes.Clone(captured.validOutcome.Effects),
				},
				Violating: observation.Outcome{
					Output:       bytes.Clone(captured.violatingOutcome.Output),
					FailureClass: bytes.Clone(captured.violatingOutcome.FailureClass),
					Effects:      bytes.Clone(captured.violatingOutcome.Effects),
				},
			},
		}},
		Oracle: observation.Oracle{
			Kind:         "copied-test-outcomes",
			Source:       chunkExecutionSourcePath + "#" + chunkExecutionSourceTest,
			Revision:     chunkExecutionSourceRevision,
			Producer:     "lsp-trace ordinary tests",
			Method:       "inert byte copy after subject production",
			Independence: "not established",
			Verification: "INDEPENDENTLY_REVIEWED",
			Evidence: []observation.ReviewEvidence{{
				Reviewer: "semantic-vector disposable pilot",
				Scope:    "copied bytes and exact counters only",
				Source:   chunkExecutionSourcePath,
				Revision: chunkExecutionSourceRevision,
			}},
		},
		EvidenceCeiling: observation.EvidenceCeiling{
			Version: "1",
			CannotEstablish: []string{
				"authenticity", "causality", "semantic truth", "oracle independence",
				"authority", "acceptance", "production behavior", "lsp-trace correctness",
			},
		},
		Limits: observation.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("freeze pilot: %v", err)
	}
	external := observation.ExternalObservation{
		Version:               observation.Version,
		SpecificationIdentity: frozen.Identity(),
		ValidInput:            bytes.Clone(captured.validReceipt),
		Rows: []observation.ObservedRow{{
			ID:                "strict-canonical-identity",
			ViolatingInput:    bytes.Clone(captured.violatingReceipt),
			Valid:             captured.validOutcome,
			Violating:         captured.violatingOutcome,
			ReceiptDigest:     pilotDigest(captured.validReceipt),
			ReceiptProvenance: chunkExecutionSourcePath + "#" + chunkExecutionSourceTest + "@" + chunkExecutionSourceRevision,
		}},
	}
	return frozen, external
}

func requirePilotCounters(t testing.TB, got capturedChunkExecution) {
	t.Helper()
	wantSubject := pilotCounters{Parser: 6, Validator: 20, Mutation: 19}
	if got.subject != wantSubject {
		t.Fatalf("subject counters: got %+v want %+v", got.subject, wantSubject)
	}
	if got.receipt != (pilotCounters{}) {
		t.Fatalf("receipt counters: got %+v want zero", got.receipt)
	}
}

func runObservedChunkExecutionPilot(t testing.TB) {
	t.Helper()
	wantSubject := pilotCounters{Parser: 6, Validator: 20, Mutation: 19}
	produced, err := produceStrictChunkExecution(wantSubject)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := captureProducedChunkExecution(produced)
	if err != nil {
		t.Fatal(err)
	}
	requirePilotCounters(t, captured)

	capturedSnapshot := cloneCapturedChunkExecution(captured)
	frozen, external := freezeChunkExecutionPilot(t, captured)
	frozenSnapshot := bytes.Clone(frozen.Canonical())
	externalValidInputSnapshot := bytes.Clone(external.ValidInput)
	externalViolatingInputSnapshot := bytes.Clone(external.Rows[0].ViolatingInput)
	externalValidOutcomeSnapshot := observation.ObservedOutcome{
		Output:       bytes.Clone(external.Rows[0].Valid.Output),
		FailureClass: bytes.Clone(external.Rows[0].Valid.FailureClass),
		Effects:      bytes.Clone(external.Rows[0].Valid.Effects),
	}
	externalViolatingOutcomeSnapshot := observation.ObservedOutcome{
		Output:       bytes.Clone(external.Rows[0].Violating.Output),
		FailureClass: bytes.Clone(external.Rows[0].Violating.FailureClass),
		Effects:      bytes.Clone(external.Rows[0].Violating.Effects),
	}

	mutateOriginalProducedSlices(&produced)
	if !equalCapturedChunkExecution(captured, capturedSnapshot) {
		t.Fatal("captured receipt or outcome changed after original produced slices mutated")
	}
	if !bytes.Equal(frozen.Canonical(), frozenSnapshot) {
		t.Fatal("frozen specification changed after original produced slices mutated")
	}
	if !bytes.Equal(external.ValidInput, externalValidInputSnapshot) ||
		!bytes.Equal(external.Rows[0].ViolatingInput, externalViolatingInputSnapshot) ||
		!bytes.Equal(external.Rows[0].Valid.Output, externalValidOutcomeSnapshot.Output) ||
		!bytes.Equal(external.Rows[0].Valid.FailureClass, externalValidOutcomeSnapshot.FailureClass) ||
		!bytes.Equal(external.Rows[0].Valid.Effects, externalValidOutcomeSnapshot.Effects) ||
		!bytes.Equal(external.Rows[0].Violating.Output, externalViolatingOutcomeSnapshot.Output) ||
		!bytes.Equal(external.Rows[0].Violating.FailureClass, externalViolatingOutcomeSnapshot.FailureClass) ||
		!bytes.Equal(external.Rows[0].Violating.Effects, externalViolatingOutcomeSnapshot.Effects) {
		t.Fatal("external observation changed after original produced slices mutated")
	}

	report, err := observation.AssessObservation(frozen, external)
	if err != nil {
		t.Fatalf("assess copied receipt: %v", err)
	}
	canonical, err := report.Canonical()
	if err != nil {
		t.Fatalf("canonical report: %v", err)
	}
	verified, err := observation.VerifyReport(frozen, external, canonical)
	if err != nil {
		t.Fatalf("verify canonical-equal report: %v", err)
	}
	if verified.Authority != 0 || verified.Accepted || verified.ExecutionCustody != "EXTERNAL" || verified.CausalExecution != "NOT_OBSERVED" {
		t.Fatalf("report limits: authority=%d accepted=%t custody=%s causal=%s", verified.Authority, verified.Accepted, verified.ExecutionCustody, verified.CausalExecution)
	}

	changedAuthority := bytes.Replace(canonical, []byte(`"Authority":0`), []byte(`"Authority":1`), 1)
	if bytes.Equal(changedAuthority, canonical) {
		t.Fatal("authority-byte perturbation did not change canonical report")
	}
	if _, err := observation.VerifyReport(frozen, external, changedAuthority); err == nil {
		t.Fatal("changed authority byte verified")
	}
	requirePilotCounters(t, captured)
}

func TestChunkExecutionCaptureRejectsUnproduced(t *testing.T) {
	if _, err := captureProducedChunkExecution(producedChunkExecution{}); err == nil || err.Error() != "capture before subject production" {
		t.Fatalf("unproduced capture: %v", err)
	}
}

func TestChunkExecutionObservationReceiptPilot(t *testing.T) {
	runObservedChunkExecutionPilot(t)
}

func BenchmarkChunkExecutionStrictSubjectBaseline(b *testing.B) {
	want := pilotCounters{Parser: 6, Validator: 20, Mutation: 19}
	for i := 0; i < b.N; i++ {
		produced, err := produceStrictChunkExecution(want)
		if err != nil {
			b.Fatal(err)
		}
		if !produced.produced || produced.subject != want {
			b.Fatalf("baseline guard: produced=%t subject=%+v", produced.produced, produced.subject)
		}
	}
}

func BenchmarkChunkExecutionObservedCopiedReceipt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		runObservedChunkExecutionPilot(b)
	}
}
