package vector_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	vector "github.com/jaresty/semantic-vector/vector"
)

type request struct {
	Authorized bool `json:"authorized"`
}
type codec[T any] struct{}

func (codec[T]) Encode(v T) ([]byte, error) { return json.Marshal(v) }
func (codec[T]) Decode(b []byte) (T, error) { var v T; err := json.Unmarshal(b, &v); return v, err }

type observations struct{}

func (observations) EncodeOutput(v string) ([]byte, error)  { return json.Marshal(v) }
func (observations) EncodeFailure(v string) ([]byte, error) { return json.Marshal(v) }
func (observations) EncodeEffects(v []string) ([]byte, error) {
	if v == nil {
		v = []string{}
	}
	return json.Marshal(v)
}

type access struct{}

func (access) Execute(_ context.Context, r request) (vector.Observation[string, string, string], error) {
	if r.Authorized {
		return vector.Observation[string, string, string]{Output: "created", Effects: []string{"create"}}, nil
	}
	return vector.Observation[string, string, string]{Failure: "forbidden", Effects: []string{}}, nil
}
func hash(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func ExampleRunner() {
	valid, _ := json.Marshal(request{true})
	violating, _ := json.Marshal(request{false})
	outOK, _ := json.Marshal("created")
	outZero, _ := json.Marshal("")
	failZero, _ := json.Marshal("")
	failDenied, _ := json.Marshal("forbidden")
	create, _ := json.Marshal([]string{"create"})
	zero, _ := json.Marshal([]string{})
	v := vector.Vector[request, string, string, string]{Version: vector.Version, Property: vector.Property{ID: "authorization", Statement: "unauthorized requests are refused", Source: "contract"}, Oracle: vector.Oracle{Kind: "reference", Source: "contract", Revision: "1", Producer: "reviewer", Method: "manual", Review: "reviewed", Independence: vector.Reference}, Codec: codec[request]{}, ObservationCodec: observations{}, Valid: vector.World[request]{Canonical: valid}, Perturbation: vector.Perturbation[request]{ID: "remove-authorization", Difference: vector.WholeWorldDifference{BeforeDigest: hash(valid), AfterDigest: hash(violating)}, Apply: func(r request) (request, error) { r.Authorized = false; return r, nil }}, ExpectedValid: vector.Expectation[string, string, string]{Output: "created", Effects: vector.Effects[string]{Known: true, Values: []string{"create"}}, Classification: "allowed", Canonical: vector.CanonicalExpectation{Output: outOK, Failure: failZero, Effects: create}}, ExpectedViolating: vector.Expectation[string, string, string]{Failure: "forbidden", Effects: vector.Effects[string]{Known: true, Values: []string{}}, Classification: "denied", Canonical: vector.CanonicalExpectation{Output: outZero, Failure: failDenied, Effects: zero}}, EvidenceCeiling: vector.EvidenceCeiling{Version: "v0", CannotEstablish: []string{"all authorization behavior"}}}
	classify := func(o vector.Observation[string, string, string]) (string, error) {
		if o.Failure != "" {
			return "denied", nil
		}
		return "allowed", nil
	}
	r := vector.Runner[request, string, string, string]{Adapter: access{}, Classify: classify, Limits: vector.DefaultLimits()}.Run(context.Background(), v)
	fmt.Println(r.Status, r.Contrast, r.OracleQualified)
	// Output: PASS ESTABLISHED true
}
