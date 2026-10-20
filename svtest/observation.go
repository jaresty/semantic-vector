package svtest

import observation "github.com/jaresty/semantic-vector/observation"

// ExternalObservedRow converts observations already produced by ordinary test
// code into a sidecar row. It does not execute, classify, encode, or qualify.
func ExternalObservedRow(id string, violatingInput []byte, valid, violating observation.ObservedOutcome, receiptDigest, receiptProvenance string) observation.ObservedRow {
	return observation.ObservedRow{
		ID: id, ViolatingInput: append([]byte(nil), violatingInput...),
		Valid: cloneObserved(valid), Violating: cloneObserved(violating),
		ReceiptDigest: receiptDigest, ReceiptProvenance: receiptProvenance,
	}
}

func cloneObserved(x observation.ObservedOutcome) observation.ObservedOutcome {
	return observation.ObservedOutcome{Output: append([]byte(nil), x.Output...), FailureClass: append([]byte(nil), x.FailureClass...), Effects: append([]byte(nil), x.Effects...)}
}
