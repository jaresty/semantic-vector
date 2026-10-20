# ChunkExecution observation receipt pilot

This disposable test/docs-only pilot demonstrates how semantic-vector can assess inert, caller-supplied copies of already-produced ChunkExecution receipt bytes and outcomes. It does not import or execute lsp-trace code: Go's `internal` package boundary prohibits that import, and the pilot intentionally uses only the published `github.com/jaresty/semantic-vector/observation` API.

## Pinned source and baseline

The sole lsp-trace oracle remains its ordinary test suite. This pilot is bound to:

- repository checkout: `/private/tmp/adr0011-v8-local-main-integration`;
- source revision: `597dcd1bef1b161c3f8fb3083cc57a51436b52e6`;
- source file: `internal/censuschunking/execution_test.go`;
- strict test: `TestChunkExecutionReceiptV1StrictCanonicalIdentity`.

At that revision, the strict test establishes the baseline used here: 6 parser calls, 20 validator calls, and 19 mutation calls. The pilot does not reproduce or replace that oracle. Its local fixture merely preserves those already-established counts as copied values.

## Receipt boundary

The pilot's sequence is deliberately one-way:

1. the subject fixture produces copied canonical receipt bytes, copied outcomes, and subject counters;
2. capture rejects a value not marked produced and then clones the produced bytes;
3. the captured/observed subject counters remain exactly 6 parser, 20 validator, and 19 mutation calls;
4. receipt parser, validator, and mutation counters remain exactly 0, 0, and 0;
5. `observation.AssessObservation` creates a report from the frozen specification and inert external observation;
6. `observation.VerifyReport` verifies canonical-equal report bytes without any producer callback surface.

The test records one producer call before capture and asserts that the count is unchanged after `VerifyReport`. This is a bounded API observation: the published verification function accepts only a frozen specification, an external observation, and report bytes; it has no producer callback parameter.

The positive case verifies exact canonical report bytes. The negative case changes only the canonical report's authority byte from `0` to `1` and requires verification to reject it. A successful verified report must retain the API's exact vocabulary and limits:

- `Authority == 0`;
- `Accepted == false`;
- `ExecutionCustody == "EXTERNAL"`;
- `CausalExecution == "NOT_OBSERVED"`.

## Guard sensitivity

Before the final commit, the expected parser count is temporarily perturbed from 6 to 7. The focused test must fail at the named subject-counter guard, after which the expected count is restored to 6 and the same focused test must pass. That observe/perturb/observe/restore/observe cycle demonstrates that the retained guard detects the specific counter mismatch; it does not validate lsp-trace itself.

## Benchmarks

Two benchmarks are included:

- `BenchmarkChunkExecutionStrictSubjectBaseline` measures construction and checking of the pinned strict-subject fixture;
- `BenchmarkChunkExecutionObservedCopiedReceipt` measures copied capture, observation assessment, canonicalization, positive verification, negative authority-byte verification, and counter checks.

Every benchmark iteration executes correctness guards. Benchmark results are descriptive measurements of this disposable fixture on the reporting machine only. They do not establish comparative significance, production performance, performance causality, or a stable overhead ratio.

## Explicit limitations

This pilot does **not** establish:

- authenticity of the copied receipt bytes or outcomes;
- causality between lsp-trace execution and the copied values;
- semantic truth of the declared property or outcomes;
- oracle independence;
- authority for any decision;
- acceptance of a product, release, result, or receipt;
- production behavior or production readiness; or
- lsp-trace correctness.

It also does not authenticate provenance merely because provenance strings are present. semantic-vector verifies canonical byte relationships and report recomputation within its published evidence ceiling. Ordinary lsp-trace tests remain the sole oracle for lsp-trace behavior and correctness.
