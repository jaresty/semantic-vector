# v1 implementation scope

The current implementation is a narrow in-memory semantic-contrast library.
It provides typed properties, canonical worlds, codec-isolated perturbations,
canonical expectations, pre-binding canonical-data snapshots, adapters,
explicit classifications, exact and typed failure comparison, declared
provenance plus bounded independent-review evidence, executable custody keys,
evidence ceilings, versioned identities, lightweight contrasts, and bounded
deterministic reports.

## Intentional narrowing

- Callers supply deterministic world and observation codecs. Values whose codec
  cannot repeatedly produce identical bounded bytes are unsupported.
- Controlled difference is exact whole-world SHA-256 before/after digests.
  Generic field paths and semantic-dimension claims are deferred.
- Valid and violating values are independently decoded from canonical bytes;
  caller-owned mutable values are never executed directly.
- Contrast is established only by distinct declared classifications that match
  distinct observed classifier results. Incidental observation inequality does
  not establish contrast.
- Core freeze distinguishes omitted effects from typed `NoEffects`; the
  lightweight contrast API may default omitted effects to asserted empty lists.
- Failure classes and matcher keys are canonical declarations. Runtime matcher
  functions are registry-bound, nonserializable, and excluded from identity.
  Failure-matcher panic alone is contained; codec, perturbation, adapter, and
  classifier panics propagate by policy. Captured closure state remains mutable.
- Oracle declarations default to `UNVERIFIED`; bounded review evidence is
  represented separately and never proves author independence or domain truth.
- Reports render library-owned encoded bytes and fixed findings only, under
  explicit mismatch and render limits. They do not invoke domain formatters or
  infer causal defect ownership; authoritative attribution remains `UNRESOLVED`,
  while optional caller claims are labeled `DECLARED_UNVERIFIED`.
- Canonical data is deeply copied, but codecs, mutators, matchers, adapters, and
  classifiers remain trusted executable dependencies. Concurrency safety is
  conditional on every retained dependency and captured value.
- V1 rejects v0 on new identity/execution paths. Dedicated golden-tested helpers
  preserve historical v0 identity and structured-report encoding only.
- Additive `semantic-vector-batch/v1` preflights and sorts immutable mutation IDs
  before executable closures, snapshots one shared valid world plus independently
  authored property/mutation/violating semantics, executes valid once and each
  mutation once, and preserves `RunContrast`-equivalent per-row reports. Shared
  observation is admitted only with an adapter-bound caller declaration labeled
  `DECLARED_UNVERIFIED`; no purity, replay, reentrancy, or custody claim is verified.
  Compact bytes are strict summaries with explicit omissions, not complete reports
  or oracle authority; parsing establishes semantic shape, while relational binding
  requires `VerifyCompactBatch` against canonical full-result bytes.

The following remain explicitly deferred and are not implied by the v1 API:
fixture generators; JSON fixture transport or public canonical schemas; a
general resource-limit framework beyond v1 codec/report bounds; completeness
manifests or semantic-completeness claims; generic concurrency scheduling or CAS
helpers; and any `lsp-trace` integration or dependency.
