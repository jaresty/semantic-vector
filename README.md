# semantic-vector

`semantic-vector` is a Go library for qualifying caller-supplied external observations against a frozen specification.

The [`observation`](observation) package:

1. freezes a typed specification into canonical bytes;
2. accepts observation literals supplied by the caller;
3. assesses those supplied outcomes against the frozen expectations; and
4. verifies a canonical report by recomputing the complete assessment.

A matching report is bounded evidence about the supplied bytes. Every report records external custody, `NOT_OBSERVED` causal status, authority `0`, and `accepted=false`.

## Nonclaims

The library does not:

- run the subject being assessed;
- invoke caller callbacks to obtain outcomes;
- establish that the supplied outcomes came from a real system;
- establish causality or production behavior;
- establish property truth, semantic completeness, or oracle independence;
- authenticate receipt provenance, reviewers, or custody; or
- grant product, release, publication, or decision authority.

`ParseReportSyntax` checks only strict canonical syntax. `VerifyReport` is the semantic binding path: it recomputes the assessment from the frozen specification and the supplied observation, then requires exact canonical equality.

See [`docs/PRD.md`](docs/PRD.md) for the product contract, [`docs/SCOPE.md`](docs/SCOPE.md) for the implemented boundary, [`docs/MIGRATION.md`](docs/MIGRATION.md) for the source-breaking simplification, and [`observation/example_test.go`](observation/example_test.go) for a complete example.

## Development

```bash
go test ./...
```

## License

Licensed under the [MIT License](LICENSE). Copyright (c) 2026 jaresty.
