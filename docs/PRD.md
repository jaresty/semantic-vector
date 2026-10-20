# Product Requirements: external observation qualification

- **Status:** Implemented narrow product
- **Module:** `github.com/jaresty/semantic-vector`
- **Public package:** `observation`

## Purpose

The product qualifies caller-supplied external observations against a frozen specification. It makes the binding, classification, provenance declarations, evidence ceiling, and report identity deterministic and independently inspectable.

The product accepts two kinds of input:

- a typed observation specification containing canonical valid and violating inputs, declared properties, expected outcomes, oracle metadata, review evidence, and an evidence ceiling; and
- an external observation literal containing caller-supplied canonical inputs and outcomes.

It produces a deterministic report that classifies the supplied observation against the frozen specification.

## Required behavior

1. `FreezeObservationSpecification` validates the complete specification, stable canonical encoding, bounded work, ordered row identities, exact digests, and declared byte spans before returning an immutable canonical snapshot.
2. `AssessObservation` compares an external observation literal with that snapshot and returns a bounded classification.
3. Reports always identify custody as `EXTERNAL`, causal status as `NOT_OBSERVED`, authority as `0`, and acceptance as `false`.
4. `ParseReportSyntax` validates canonical report syntax without exposing a semantically accepted report.
5. `VerifyReport` recomputes the report from the frozen specification and external observation and requires exact canonical equality.
6. Canonical specification and report identities are deterministic within `semantic-vector-observation/v1`.
7. Limits bound canonical bytes, report bytes, rows, and work.
8. The generic report golden and canonical MIT license test remain stable guards.

## Classification

The report classification is one of:

- `SPEC_INVALID`;
- `OBSERVATION_MALFORMED`;
- `OBSERVATION_MISMATCH`;
- `SUPPLIED_OUTCOMES_MATCH_QUALIFIED_SPECIFICATION`;
- `NON_DISCRIMINATING`; or
- `UNQUALIFIED`.

A matching classification means only that canonically bound supplied outcomes match qualified declarations and discriminate the declared worlds.

## Evidence boundary

Verified byte spans establish only the canonical byte-difference witness. Declared property meaning remains `DECLARED_UNVERIFIED`. Oracle review metadata records caller-supplied provenance; it does not authenticate the producer, reviewer, source, revision, or independence claim.

## Nonclaims

The product does not run the assessed subject, obtain outcomes through callbacks, prove that supplied evidence came from a real system, establish causal behavior, establish production behavior, prove domain truth, prove semantic completeness, authenticate custody, assign defect responsibility, or grant acceptance or decision authority.

## Acceptance criteria

- The public observation API and schema identity remain stable.
- A public example freezes a specification, supplies an external observation literal, assesses it, checks the bounded classification and nonclaims, canonicalizes the report, and verifies it.
- The repository contains no competing product surface.
- All Go tests, race checks, vet checks, builds, package listing, dependency listing, stale-reference checks, license checks, and diff checks pass.
