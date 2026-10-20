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

## Report interpretation

V1 reports independently state:

- `expectation_mismatch`: an observed value differed from its declaration;
- `execution_failure`: adapter, classifier, or matcher execution failed;
- `test_definition_defect`: the vector or its declared machinery failed validation;
- `discrimination`: the observed contrast result; and
- `attribution`: authoritative `UNRESOLVED` in v1; and
- `attribution_claim`: optional caller-declared `DECLARED_UNVERIFIED` diagnostic metadata.

These are bounded test observations, not proof of domain truth, product acceptance, semantic completeness, or causal responsibility.
