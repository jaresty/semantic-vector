# v0 implementation scope

The current implementation is a narrow in-memory semantic-contrast library.
It provides typed properties, canonical worlds, codec-isolated perturbations,
canonical expectations, adapters, explicit classifications, exact comparison,
oracle qualification, evidence ceilings, stable vector identities, and bounded
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
- Known zero effects normalize nil and empty slices to one canonical empty list.
- Reports render library-owned encoded bytes and fixed findings only, under
  explicit mismatch and render limits. They do not invoke domain formatters.

The following remain explicitly deferred and are not implied by the v0 API:
fixture generators; JSON fixture transport or public canonical schemas; a
general resource-limit framework beyond v0 codec/report bounds; completeness
manifests or semantic-completeness claims; generic concurrency scheduling or CAS
helpers; and any `lsp-trace` integration or dependency.
