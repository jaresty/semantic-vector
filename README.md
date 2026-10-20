# semantic-vector

`semantic-vector` is a proposed Go library and testing technique for executable contract evidence.

A semantic vector binds:

- a governed property;
- one valid semantic world;
- one controlled violating world;
- exact canonical input and expected output bytes;
- typed expected failure;
- explicit expected effects and prohibited effects;
- oracle provenance and evidence ceiling.

The project now contains a narrow, functional **v1 in-memory library** plus explicit v0 identity/report compatibility encoders. It is not yet suitable for production use; fixture transport, resource limits, completeness manifests, concurrency helpers, and integrations remain deferred.

## Status

- Product requirements: [`docs/PRD.md`](docs/PRD.md)
- API package: [`vector`](vector)
- Implemented scope: [`docs/SCOPE.md`](docs/SCOPE.md)
- Migration notes: [`docs/MIGRATION.md`](docs/MIGRATION.md)
- `lsp-trace` adoption: deferred, absent, and separately gated

## Design principle

> A vector is complete only when the same guard distinguishes a valid semantic world from a controlled violating world and accounts for both result and effects.

## Development

```bash
go test ./...
```

The package validates and snapshots canonical data before adapters can be bound. `semantic-vector/v1` supports verified whole-world differences, independently decoded worlds, declared-versus-reviewed provenance, executable custody keys, typed failure matchers with matcher-panic containment, explicit `NoEffects`, one-call or staged two-world contrasts, observation-only result classification, and bounded deterministic reports.

The additive `semantic-vector-batch/v1` API freezes one shared valid world plus a bounded nonempty set of independently authored controlled mutations, preflights and sorts immutable mutation IDs before invoking executable closures, executes the valid world once and each violating world once, and returns one shared valid result plus `RunContrast`-equivalent per-mutation reports retaining each authored property and violating expectation. `BatchPlan` keeps shared semantic declarations explicit, while its template shares only codec/observation-codec custody and adapter/classifier binding; every row explicitly carries property, mutation, stable perturbation key, violating outcome, and effects. Shared valid observation requires an adapter-bound `DECLARED_UNVERIFIED` reuse declaration; it records caller assumptions about scope, purity, replay, and reentrancy but does not verify them. Compact reports are strict canonical summaries with explicit omissions, not complete reports; use `VerifyCompactBatch` rather than parsing alone to establish their binding to canonical full-result bytes.

A data snapshot does not freeze retained codec, perturbation, matcher, adapter, or classifier closures. Their trust and concurrency safety remain caller responsibilities. Only failure-matcher panic is contained; other executable panics propagate by policy. The library records claims and bounded review evidence; it cannot authenticate custody keys or prove author independence, reviewer identity, domain truth, causal defect ownership, product acceptance, or semantic completeness. Authoritative attribution stays unresolved; optional caller claims are explicitly declared-unverified. V1 rejects v0 on new execution and identity paths; explicit golden-tested legacy helpers preserve only historical v0 identity and report encoding.

## License

No license has been selected yet. Until a license file is added, normal copyright restrictions apply.
