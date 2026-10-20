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
	producerCalls    int
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
		subject:       pilotCounters{Parser: 6, Validator: 20, Mutation: 19},
		producerCalls: 1,
		produced:      true,
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

	frozen, external := freezeChunkExecutionPilot(t, captured)
	report, err := observation.AssessObservation(frozen, external)
	if err != nil {
		t.Fatalf("assess copied receipt: %v", err)
	}
	canonical, err := report.Canonical()
	if err != nil {
		t.Fatalf("canonical report: %v", err)
	}
	producerCallsBeforeVerify := produced.producerCalls
	verified, err := observation.VerifyReport(frozen, external, canonical)
	if err != nil {
		t.Fatalf("verify canonical-equal report: %v", err)
	}
	if produced.producerCalls != producerCallsBeforeVerify {
		t.Fatalf("VerifyReport invoked producer: before=%d after=%d", producerCallsBeforeVerify, produced.producerCalls)
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
		if !produced.produced || produced.subject != want || produced.producerCalls != 1 {
			b.Fatalf("baseline guard: produced=%t subject=%+v producer_calls=%d", produced.produced, produced.subject, produced.producerCalls)
		}
	}
}

func BenchmarkChunkExecutionObservedCopiedReceipt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		runObservedChunkExecutionPilot(b)
	}
}
