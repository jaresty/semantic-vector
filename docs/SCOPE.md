# Implemented scope

The repository has one purpose: qualify caller-supplied external observations against frozen specifications.

## Included

The `observation` package includes:

- typed specification, property, expected-outcome, oracle, review-evidence, and evidence-ceiling values;
- stable caller-provided canonical input encoding;
- bounded specification freezing and defensive copies;
- exact canonical input digests and byte-span verification;
- external observation literals;
- deterministic assessment classifications;
- canonical report identity and encoding;
- strict syntax-only report parsing;
- bound report verification by complete recomputation;
- bounded multi-observation assessment; and
- typed validation issues and contained codec panic classification.

## Fixed report limits

Every assessment report states:

- `ExecutionCustody == "EXTERNAL"`;
- `CausalExecution == "NOT_OBSERVED"`;
- `Authority == 0`; and
- `Accepted == false`.

These values are constraints, not placeholders for stronger authority.

## Excluded claims and capabilities

The package has no subject-running path and no callback for obtaining observed outcomes. It does not establish source-system activity, causal behavior, production behavior, property truth, semantic completeness, authenticated provenance, reviewer independence, defect ownership, release readiness, publication authority, or product acceptance.

Strict syntax parsing does not establish semantic binding. A report is bound only when `VerifyReport` successfully recomputes the exact assessment from the same frozen specification and external observation.

The package verifies canonical byte differences, not the semantic meaning assigned to those differences. Property statements and provenance remain caller declarations within the report's explicit evidence ceiling.
