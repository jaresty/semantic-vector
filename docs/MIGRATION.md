# Migration from semantic-vector/v0 to semantic-vector/v1

`semantic-vector/v1` is an explicit canonical transition. Verification evidence, typed failure declarations, executable custody keys, binding custody, and report classification change identity and report bytes. The v1 APIs reject `semantic-vector/v0`; they never reinterpret old content under the new schema.

## Preserved v0 operations

- `LegacyV0Identity` reproduces the historical v0 identity metadata shape.
- `LegacyV0StructuredReport` reproduces the historical v0 structured-report field set.
- Golden fixtures under `vector/testdata/v0` pin both encodings.

There is no v0 execution wrapper. `Runner.Run`, `Runner.Identity`, `FreezeData`, `Freeze`, `FreezeContrast`, and `RunContrast` reject v0. Historical `Runner` status behavior is therefore not silently preserved: callers must retain an old library version to execute v0, or migrate the vector explicitly to v1. `Oracle.Qualified` remains callable but now conservatively requires explicit bounded verification evidence.

## Source breaks

Go positional composite literals break when exported structs gain fields. Migrate every positional literal to keyed fields, including:

- `Oracle`: adds `Verification`;
- `Expectation`: adds `FailureExpectation`;
- `Vector`: adds `Executables`;
- `Report`: adds `Classification`, `AttributionClaim`, and `Binding`;
- `Report.Classification` adds v1 observation and attribution fields; the committed v0 report had no causal defect booleans to replace.

`Version` now aliases `VersionV1` (`semantic-vector/v1`). Use `VersionV0` only with explicit legacy encoding helpers.

## Semantic breaks

- A legacy `Review` string or `Independence: reference` declaration never qualifies an oracle.
- Verification defaults to `UNVERIFIED`.
- V1 identity includes verification evidence, typed failure declarations, and executable custody keys.
- New runners reject v0 instead of changing its identity silently.
- Mismatch and nondiscrimination do not attribute a defect to an implementation.
- Adapter, classifier, and matcher failures are reported as execution failures, not implementation defects.
- Authoritative attribution remains `UNRESOLVED` in v1. `DeclareAttribution` records only a separate `DECLARED_UNVERIFIED` caller claim; strings never establish review or verification. Deprecated `Adjudicate` has the same non-authoritative behavior.
- Omitted effects remain distinct from `NoEffects`; lightweight contrast helpers may apply an explicit no-effects default.

## Canonical data snapshot and executable custody

Prefer the staged v1 core:

```go
snapshot, err := vector.FreezeData(spec, registry, vector.DefaultLimits())
bound, err := vector.BindTrusted(
    snapshot,
    "adapter/access/v1", adapter,
    "classifier/access/v1", classify,
)
report := bound.Run(ctx)
```

`FreezeData` deep-copies library-owned canonical byte slices, effect slices, evidence slices, ceiling slices, and exact-message data. Post-snapshot mutation of those caller-owned data aliases is inert.

The snapshot retains trusted executable dependencies. It does **not** freeze or deep-copy closure state in:

- `Codec` and `ObservationCodec` implementations;
- `Perturbation.Apply`;
- registered failure matchers;
- bound adapters; or
- bound classifiers.

V1 requires stable codec, observation-codec, and perturbation keys through `ExecutableCustody`, plus an explicit `ClosureStateRetained: true` acknowledgement. `BindTrusted` requires stable adapter and classifier keys. Matcher keys are canonical; matcher functions are snapshotted from the registry but any state captured by those functions remains mutable.

The library cannot authenticate these keys, prove that implementations match them, prove author independence, or make executable dependencies concurrency-safe. Concurrent use is safe only when every retained codec, matcher, adapter, classifier, and captured value is independently concurrency-safe. The registry itself is synchronized, and replacing a registry entry does not replace a function already captured by a data snapshot.

`Freeze` and `Bind` remain compatibility aliases. `Bind` marks its report binding as unkeyed compatibility custody.

## Typed failures and panic containment

`FailureExpectation` separates stable class, optional exact message, and optional matcher key. `ClassifiedFailure` supplies the in-memory class; matchers can use `errors.Is`.

```go
registry := vector.NewFailureRegistry()
_ = registry.Register("permission-denied/v1", func(err error) bool {
    return errors.Is(err, ErrPermissionDenied)
})
```

Matcher functions are excluded from canonical bytes. Missing and duplicate keys fail closed. Failure-matcher panic alone is recovered as typed `MATCHER_PANIC`; bound execution reports it as an execution failure instead of crashing. `MatchResult` preserves that typed error; compatibility `Match` returns false on panic. Panics from codecs, perturbations, adapters, and classifiers propagate by policy; v1 does not claim a general executable panic boundary.

## Lightweight one-call execution

`RunContrast` combines defaulting, `FreezeData`, `BindTrusted`, and `Run`:

```go
report, err := vector.RunContrast(ctx, vector.ContrastRun[Input, Output, Failure, Effect]{
    Vector:        spec,
    Registry:      registry,
    AdapterKey:    "adapter/access/v1",
    Adapter:       adapter,
    ClassifierKey: "classifier/access/v1",
    Classify:      classify,
})
```

It still requires a complete explicit `Vector`, including oracle provenance, controlled mutation, canonical expectations, effect assertions, and executable custody. Its benefit is safer defaults and less staging boilerplate, not reduced domain-specification LOC.

## Additive batch/v1 execution

Existing v0/v1 single-pair APIs and encodings are unchanged. New callers may use `RunBatchContrast` with one canonical valid world, a nonempty bounded mutation list, shared adapter/classifier custody, and an explicit `ValidObservationReuse` declaration whose status is `DECLARED_UNVERIFIED`.

The exact amortization boundary is intentional. These fields must match because the batch executes and reuses one valid observation: valid canonical input; expected-valid canonical output/failure/effects, classification, and typed-failure declaration; codec and observation-codec custody keys; adapter and classifier keys; oracle provenance and verification evidence; and the evidence ceiling. Each row independently retains its complete `Property` (ID, statement, source, version), perturbation ID/difference/key/function, and expected-violating output/failure/effects/classification/typed-failure semantics. Consequently every returned row report retains its authored property and is equivalent to running that authored vector through `RunContrast`; the unchanged 24-row pilot does not flatten properties or violating expectations.

Raw immutable mutation IDs are checked for absence/duplicates and sorted before any codec, mutator, matcher-freeze, or other retained executable closure is invoked. Preparation, `Apply`, snapshot construction, execution, failures, reports, and identity therefore follow stable mutation-ID order. Duplicate IDs fail with zero closure calls. Duplicate violating-world digests and other semantic conflicts are checked after ordered preparation.

Batch identity uses the separate `semantic-vector-batch/v1` domain and a pre-binding canonical snapshot; it never changes `semantic-vector/v1` pair identities. `MaxTotalCanonicalBytes` is exactly the byte length of that canonical JSON snapshot: batch version; one shared valid world and expected-valid canonical data; shared oracle, codec/observation-codec and adapter/classifier custody, ceiling, and reuse declaration; then each ordered row's property, mutation declaration, perturbation custody key, violating world, and expected-violating canonical data—each encoded once. Checked length comparison admits the exact bound and rejects one byte less. Operational limits are reported but are not recursively embedded in the snapshot they bound.

For direct migration, `BatchPlan` keeps shared semantic declarations on the plan, while `BatchTemplate` contains only shared codec/observation-codec custody and adapter/classifier binding. Every `BatchRow` explicitly carries its property, mutation, stable perturbation key, and expected-violating outcome/effects. Empty, duplicate, or inconsistent row-key custody is rejected before mutation closures are invoked, and full and compact results preserve each exact row key.

Tests may instead import `svtest` and configure the same shared declarations once as a `Contract`. `Reject` rows require explicit effects; `RejectNoEffects` is the named assertion of an empty typed effect list and does not generate canonical effect bytes. Expected-valid effects, the nonzero evidence ceiling, and the adapter-bound `DECLARED_UNVERIFIED` reuse declaration are preflighted before any retained codec, mutation, adapter, or classifier executes. `CheckMatrix` invokes one `BatchPlan`, then reports retained results through stable named subtests without rerunning mutations. Expected status and contrast stay caller-authored because oracle verification can intentionally make a fully matched contrast report `FAILED`. Canonical full/compact bytes are generated only when the corresponding writer option is supplied, and returned bytes are the unchanged core encodings. The package is test-only by documentation and policy, not by a Go dependency restriction.

For the retained 24-row parser pilot, focused handwritten declaration/invocation measured 19 nonblank gofmt lines versus 17 for `svtest`. Five-sample benchmark observations were approximately 1.7x runtime (about 1.68x in the review sample and 1.72x in a local rerun), 3.42x allocated bytes, and 1.30x allocations relative to the handwritten loop. These figures are scoped pilot evidence, not general guarantees. Full and compact serialization remained disabled on the default benchmark path; core validation and retained evidence dominate the remaining cost.

Real migration evidence is retained outside the repository at `/private/tmp/semantic-vector-batch-migration-evidence`. Its copied 24-row pilot measured, on Apple M5 Max with five `go test -bench ... -benchmem` samples, handwritten execution at 521,622–539,241 ns/op and 7,675 allocs/op, BatchPlan at 868,642–900,887 ns/op and 9,930–9,933 allocs/op, and pairwise semantic vectors at 1,425,735–1,489,518 ns/op and 19,866–19,867 allocs/op. The same fixture produced a 48,987-byte full report and 5,130-byte compact report, with hashes recorded in that evidence directory. Source measurements use UTF-8 bytes and nonblank physical lines in named gofmt-formatted function regions while excluding shared helpers; they are observations of this pilot, not guarantees of LOC reduction or future performance. The retained `row25-edit.diff` records the actual one-line 25th mutation edit.

The reuse declaration does not prove adapter purity, replay safety, reentrancy, or custody. Adapter and classifier panic behavior remains the single-pair policy: only matcher panic is contained.

`BatchResult.Structured` is the full deterministic batch result. `BatchResult.Compact` is a measured canonical summary bound by `full_result_digest`; it avoids repeated valid-world and per-pair metadata, lists every omitted detail class, and must not be described as complete. `ParseCompactBatch` establishes only strict canonical syntax and semantic shape: known enums, SHA-256 syntax, stable order, nonempty declarations/omissions/ceilings, and meaningful positive limits. It does not establish external full-result binding. `VerifyCompactBatch` recomputes the full digest and relationally verifies batch identity, mutation order/count/IDs, outcomes, classifications, effects digests, provenance, ceiling, reuse declaration, and limits against canonical full-result bytes.

## Report interpretation

V1 reports independently state:

- `expectation_mismatch`: an observed value differed from its declaration;
- `execution_failure`: adapter, classifier, or matcher execution failed;
- `test_definition_defect`: the vector or its declared machinery failed validation;
- `discrimination`: the observed contrast result; and
- `attribution`: authoritative `UNRESOLVED` in v1; and
- `attribution_claim`: optional caller-declared `DECLARED_UNVERIFIED` diagnostic metadata.

These are bounded test observations, not proof of domain truth, product acceptance, semantic completeness, or causal responsibility.
