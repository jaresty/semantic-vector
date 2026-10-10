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

The project now contains a narrow, functional **v0 in-memory library**. It is not yet suitable for production use; fixture transport, resource limits, completeness manifests, concurrency helpers, and integrations remain deferred.

## Status

- Product requirements: [`docs/PRD.md`](docs/PRD.md)
- API package: [`vector`](vector)
- Implemented scope: [`docs/SCOPE.md`](docs/SCOPE.md)
- `lsp-trace` adoption: deferred, absent, and separately gated

## Design principle

> A vector is complete only when the same guard distinguishes a valid semantic world from a controlled violating world and accounts for both result and effects.

## Development

```bash
go test ./...
```

The v0 package validates and executes codec-backed in-memory vectors. It supports verified whole-world differences, independently decoded worlds, explicit contrast classifications, non-masking custody findings, and bounded deterministic reports. Its reports remain bounded test evidence and do not establish product acceptance or semantic completeness.

## License

No license has been selected yet. Until a license file is added, normal copyright restrictions apply.
