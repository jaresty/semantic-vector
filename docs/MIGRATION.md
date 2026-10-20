# Migration to observation-only semantic-vector

The repository now exposes only the `observation` package for qualifying caller-supplied external observations.

## Removed surface

The former `vector` and `svtest` packages were removed in full, including legacy v0 fixtures and APIs for runners, adapters, perturbations, contrast execution, shared-valid batch execution, compatibility encoders, and test-matrix helpers.

There is no archive tag and no compatibility layer. Consumers that still need a removed package or API must keep using a revision that contained it while they migrate; this repository does not retain or recreate that surface.

## Replacement usage

Callers now:

1. construct an `observation.ObservationSpecification`;
2. freeze it with `observation.FreezeObservationSpecification`;
3. construct `observation.ExternalObservation` directly with `observation.ObservedRow` and `observation.ObservedOutcome` literals;
4. assess it with `observation.AssessObservation`; and
5. canonicalize and verify the report with `observation.VerifyReport`.

There is intentionally no replacement for `svtest.ExternalObservedRow`. Callers use observation literals so custody remains explicit and no helper appears to obtain or qualify the outcomes.

The observation schema and identity domain remain `semantic-vector-observation/v1`. The Go module path remains `github.com/jaresty/semantic-vector`.
