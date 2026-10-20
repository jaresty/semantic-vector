package observation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jaresty/semantic-vector/observation"
)

type accessInput struct {
	Allowed bool `json:"allowed"`
}

type accessCodec struct{}

func (accessCodec) Encode(input accessInput) ([]byte, error) {
	return json.Marshal(input)
}

func exampleDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func Example_externalObservationQualification() {
	validInput := []byte(`{"allowed":true}`)
	violatingInput := []byte(`{"allowed":false}`)
	allowed := observation.Outcome{Output: []byte(`"allowed"`), FailureClass: []byte(`""`), Effects: []byte(`[]`)}
	denied := observation.Outcome{Output: []byte(`""`), FailureClass: []byte(`"denied"`), Effects: []byte(`[]`)}

	frozen, err := observation.FreezeObservationSpecification(observation.ObservationSpecification[accessInput]{
		Version:  observation.Version,
		CodecKey: "json/access-input/v1",
		Codec:    accessCodec{},
		Valid:    accessInput{Allowed: true},
		Rows: []observation.Row[accessInput]{{
			ID:        "deny-access",
			Property:  observation.Property{ID: "deny-access", Statement: "access is denied", Source: "example", Version: "1"},
			Violating: accessInput{Allowed: false},
			Difference: observation.Difference{
				PropertyID:     "deny-access",
				BeforeDigest:   exampleDigest(validInput),
				AfterDigest:    exampleDigest(violatingInput),
				Claims:         []string{"allowed changes from true to false"},
				Spans:          []observation.ByteSpan{{BeforeStart: 11, BeforeEnd: 15, AfterStart: 11, AfterEnd: 16}},
				PropertyStatus: observation.PropertyDeclaredUnverified,
			},
			Expected: observation.Expected{Valid: allowed, Violating: denied},
		}},
		Oracle: observation.Oracle{
			Kind: "reference", Source: "example", Revision: "1", Producer: "example author", Method: "handwritten",
			Independence: "independent", Verification: "INDEPENDENTLY_REVIEWED",
			Evidence: []observation.ReviewEvidence{{Reviewer: "example reviewer", Scope: "expected outcomes", Source: "example", Revision: "1"}},
		},
		EvidenceCeiling: observation.EvidenceCeiling{Version: "1", CannotEstablish: []string{"causal execution", "production behavior", "acceptance"}},
		Limits:          observation.DefaultLimits(),
	})
	if err != nil {
		panic(err)
	}

	external := observation.ExternalObservation{
		Version:               observation.Version,
		SpecificationIdentity: frozen.Identity(),
		ValidInput:            validInput,
		Rows: []observation.ObservedRow{{
			ID:             "deny-access",
			ViolatingInput: violatingInput,
			Valid:          observation.ObservedOutcome{Output: allowed.Output, FailureClass: allowed.FailureClass, Effects: allowed.Effects},
			Violating:      observation.ObservedOutcome{Output: denied.Output, FailureClass: denied.FailureClass, Effects: denied.Effects},
		}},
	}

	report, err := observation.AssessObservation(frozen, external)
	if err != nil {
		panic(err)
	}
	canonical, err := report.Canonical()
	if err != nil {
		panic(err)
	}
	verified, err := observation.VerifyReport(frozen, external, canonical)
	if err != nil {
		panic(err)
	}

	fmt.Println(verified.Classification)
	fmt.Println(verified.ExecutionCustody)
	fmt.Println(verified.CausalExecution)
	fmt.Println(verified.Authority)
	fmt.Println(verified.Accepted)
	// Output:
	// SUPPLIED_OUTCOMES_MATCH_QUALIFIED_SPECIFICATION
	// EXTERNAL
	// NOT_OBSERVED
	// 0
	// false
}
