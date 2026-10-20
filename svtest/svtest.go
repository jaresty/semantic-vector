// Package svtest provides a test-only ergonomic layer over vector.BatchPlan.
//
// It is intended to be imported only by tests. Go cannot enforce test-only
// dependencies, so production packages must not depend on svtest. The package
// adds no oracle generation, qualification, custody, authority, or inferred
// expectations: callers still provide every semantic declaration accepted by
// vector.BatchPlan.
//
// Matrix execution inherits vector's panic policy. Only failure-matcher panics
// are contained by the core; adapter, classifier, codec, and mutation panics
// propagate.
package svtest

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jaresty/semantic-vector/vector"
)

// Contract holds declarations reused by every row in a mutation matrix.
type Contract[I, O, F, E any] struct {
	Template        vector.BatchTemplate[I, O, F, E]
	Version         string
	Oracle          vector.Oracle
	Valid           vector.World[I]
	ExpectedValid   vector.Expectation[O, F, E]
	EvidenceCeiling vector.EvidenceCeiling
	Reuse           vector.ValidObservationReuse
	Registry        *vector.FailureRegistry
	Limits          vector.BatchLimits
}

// Expected states both the violating observation and the report-level oracle.
// Status and Contrast are never inferred by svtest.
type Expected[O, F, E any] struct {
	Violating vector.Expectation[O, F, E]
	Status    vector.Status
	Contrast  vector.Contrast
}

// Row is one explicitly declared controlled mutation.
type Row[I, O, F, E any] struct {
	Name            string
	Property        vector.Property
	PerturbationKey string
	Apply           func(I) (I, error)
	Difference      vector.WholeWorldDifference
	Expected        Expected[O, F, E]
}

// Reject declares a row expected to distinguish the valid and violating worlds.
// Despite the name, effects are not defaulted; Expected.Violating.Effects must
// be explicit.
func Reject[I, O, F, E any](name string, property vector.Property, perturbationKey string, apply func(I) (I, error), difference vector.WholeWorldDifference, expected Expected[O, F, E]) Row[I, O, F, E] {
	return Row[I, O, F, E]{Name: name, Property: property, PerturbationKey: perturbationKey, Apply: apply, Difference: difference, Expected: expected}
}

// RejectNoEffects is the only constructor that supplies an effects assertion.
// It asserts an empty typed effect list while retaining caller-supplied canonical
// effect bytes; it does not encode or otherwise generate canonical evidence.
func RejectNoEffects[I, O, F, E any](name string, property vector.Property, perturbationKey string, apply func(I) (I, error), difference vector.WholeWorldDifference, expected Expected[O, F, E]) Row[I, O, F, E] {
	expected.Violating.Effects = vector.NoEffects[E]()
	return Reject(name, property, perturbationKey, apply, difference, expected)
}

// Diagnostic is a concise, row-specific comparison. Definition and execution
// failures are kept separate from observation mismatches.
type Diagnostic struct {
	Name    string
	Message string
}

// Result retains the complete core evidence. Full and Compact are populated
// only when their respective writer options are requested.
type Result struct {
	Batch       vector.BatchResult
	Diagnostics []Diagnostic
	Full        []byte
	Compact     []byte
}

type config struct {
	ctx     context.Context
	full    io.Writer
	compact io.Writer
}

// Option configures execution or opt-in audit serialization.
type Option func(*config) error

// WithContext supplies the execution context. A nil context is rejected.
func WithContext(ctx context.Context) Option {
	return func(c *config) error {
		if ctx == nil {
			return fmt.Errorf("svtest: nil context")
		}
		c.ctx = ctx
		return nil
	}
}

// WithFullReport writes and returns canonical full batch bytes after execution.
func WithFullReport(w io.Writer) Option {
	return func(c *config) error {
		if w == nil {
			return fmt.Errorf("svtest: nil full-report writer")
		}
		c.full = w
		return nil
	}
}

// WithCompactReport writes and returns canonical compact batch bytes. Compact
// generation necessarily generates the canonical full bytes internally because
// the compact report is bound to their digest.
func WithCompactReport(w io.Writer) Option {
	return func(c *config) error {
		if w == nil {
			return fmt.Errorf("svtest: nil compact-report writer")
		}
		c.compact = w
		return nil
	}
}

// RunMatrix validates definitions, invokes vector.RunBatchPlan exactly once,
// compares every row with its explicit expected status and contrast, and returns
// evidence plus concise diagnostics. It performs no report serialization unless
// requested by an option.
func RunMatrix[I, O, F, E any](contract *Contract[I, O, F, E], rows []Row[I, O, F, E], options ...Option) (Result, error) {
	var out Result
	if contract == nil {
		return out, fmt.Errorf("svtest: nil contract")
	}
	if rows == nil {
		return out, fmt.Errorf("svtest: nil rows")
	}
	if len(rows) == 0 {
		return out, fmt.Errorf("svtest: empty rows")
	}
	if err := validateContract(contract); err != nil {
		return out, err
	}
	cfg := config{ctx: context.Background()}
	for i, option := range options {
		if option == nil {
			return out, fmt.Errorf("svtest: nil option at index %d", i)
		}
		if err := option(&cfg); err != nil {
			return out, err
		}
	}
	if err := cfg.ctx.Err(); err != nil {
		return out, err
	}
	planRows, expected, err := prepareRows(rows)
	if err != nil {
		return out, err
	}
	plan := vector.BatchPlan[I, O, F, E]{
		Template: contract.Template, Version: contract.Version, Oracle: contract.Oracle,
		Valid: contract.Valid, ExpectedValid: contract.ExpectedValid,
		EvidenceCeiling: contract.EvidenceCeiling, Rows: planRows, Reuse: contract.Reuse,
		Registry: contract.Registry, Limits: contract.Limits,
	}
	out.Batch, err = vector.RunBatchPlan(cfg.ctx, plan)
	if err != nil {
		return out, err
	}
	out.Diagnostics = compare(out.Batch, expected)
	if cfg.full != nil || cfg.compact != nil {
		out.Full, err = out.Batch.Structured()
		if err != nil {
			return out, err
		}
		if cfg.full != nil {
			var n int
			if n, err = cfg.full.Write(out.Full); err != nil {
				return out, fmt.Errorf("svtest: write full report: %w", err)
			}
			if n != len(out.Full) {
				return out, fmt.Errorf("svtest: write full report: %w", io.ErrShortWrite)
			}
		}
	}
	if cfg.compact != nil {
		out.Compact, err = out.Batch.Compact()
		if err != nil {
			return out, err
		}
		var n int
		if n, err = cfg.compact.Write(out.Compact); err != nil {
			return out, fmt.Errorf("svtest: write compact report: %w", err)
		}
		if n != len(out.Compact) {
			return out, fmt.Errorf("svtest: write compact report: %w", io.ErrShortWrite)
		}
	}
	return out, nil
}

func validateContract[I, O, F, E any](contract *Contract[I, O, F, E]) error {
	if !contract.ExpectedValid.Effects.Known || contract.ExpectedValid.Effects.Values == nil {
		return fmt.Errorf("svtest: contract omits expected-valid effects; use vector.NoEffects explicitly to assert none")
	}
	ceiling := contract.EvidenceCeiling
	if strings.TrimSpace(ceiling.Version) == "" || len(ceiling.CannotEstablish) == 0 {
		return fmt.Errorf("svtest: contract requires an explicit nonzero evidence ceiling")
	}
	for _, limitation := range ceiling.CannotEstablish {
		if strings.TrimSpace(limitation) == "" {
			return fmt.Errorf("svtest: contract evidence ceiling contains an empty limitation")
		}
	}
	reuse := contract.Reuse
	if strings.TrimSpace(reuse.AdapterKey) == "" || reuse.AdapterKey != contract.Template.AdapterKey || strings.TrimSpace(reuse.Scope) == "" || reuse.Status != vector.DeclarationUnverified || len(reuse.Assumptions) == 0 {
		return fmt.Errorf("svtest: contract requires an explicit DECLARED_UNVERIFIED reuse declaration bound to the adapter")
	}
	for _, assumption := range reuse.Assumptions {
		if strings.TrimSpace(assumption) == "" {
			return fmt.Errorf("svtest: contract reuse declaration contains an empty assumption")
		}
	}
	return nil
}

type expectedRow struct {
	status   vector.Status
	contrast vector.Contrast
	class    string
	effects  []byte
}

func prepareRows[I, O, F, E any](rows []Row[I, O, F, E]) ([]vector.BatchRow[I, O, F, E], map[string]expectedRow, error) {
	planRows := make([]vector.BatchRow[I, O, F, E], 0, len(rows))
	expected := make(map[string]expectedRow, len(rows))
	keys := make(map[string]string, len(rows))
	for i, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("svtest: row %d has empty name", i)
		}
		if row.Name != name {
			return nil, nil, fmt.Errorf("svtest: row %q name has surrounding whitespace", row.Name)
		}
		if _, ok := expected[name]; ok {
			return nil, nil, fmt.Errorf("svtest: duplicate row name %q", name)
		}
		if row.Property.ID != name {
			return nil, nil, fmt.Errorf("svtest: row %q property id %q must equal row name", name, row.Property.ID)
		}
		if row.PerturbationKey == "" {
			return nil, nil, fmt.Errorf("svtest: row %q has empty perturbation key", name)
		}
		if prior, ok := keys[row.PerturbationKey]; ok {
			return nil, nil, fmt.Errorf("svtest: rows %q and %q share perturbation key %q", prior, name, row.PerturbationKey)
		}
		if row.Apply == nil {
			return nil, nil, fmt.Errorf("svtest: row %q has nil apply", name)
		}
		if row.Expected.Status == "" || row.Expected.Contrast == "" {
			return nil, nil, fmt.Errorf("svtest: row %q omits expected status or contrast", name)
		}
		if !row.Expected.Violating.Effects.Known || row.Expected.Violating.Effects.Values == nil {
			return nil, nil, fmt.Errorf("svtest: row %q omits effects; use RejectNoEffects only to assert none", name)
		}
		keys[row.PerturbationKey] = name
		expected[name] = expectedRow{row.Expected.Status, row.Expected.Contrast, row.Expected.Violating.Classification, append([]byte(nil), row.Expected.Violating.Canonical.Effects...)}
		planRows = append(planRows, vector.BatchRow[I, O, F, E]{
			Property:          row.Property,
			Mutation:          vector.Perturbation[I]{ID: name, Difference: row.Difference, Apply: row.Apply},
			PerturbationKey:   row.PerturbationKey,
			ExpectedViolating: row.Expected.Violating,
		})
	}
	return planRows, expected, nil
}

func compare(batch vector.BatchResult, expected map[string]expectedRow) []Diagnostic {
	var diagnostics []Diagnostic
	for _, mutation := range batch.Mutations {
		report := mutation.Report
		want := expected[mutation.MutationID]
		var reasons []string
		if report.Status != want.status {
			reasons = append(reasons, fmt.Sprintf("status expected=%s observed=%s", want.status, report.Status))
		}
		if report.Contrast != want.contrast {
			reasons = append(reasons, fmt.Sprintf("contrast expected=%s observed=%s", want.contrast, report.Contrast))
		}
		if report.Violating.Classification != want.class {
			reasons = append(reasons, fmt.Sprintf("class expected=%q observed=%q", want.class, report.Violating.Classification))
		}
		wantEffects := base64.RawStdEncoding.EncodeToString(want.effects)
		if report.Violating.Observation.Effects != wantEffects {
			reasons = append(reasons, fmt.Sprintf("effects expected=%q observed=%q", wantEffects, report.Violating.Observation.Effects))
		}
		if !report.Valid.Matched || !report.Violating.Matched {
			reasons = append(reasons, fmt.Sprintf("matched valid=%t violating=%t", report.Valid.Matched, report.Violating.Matched))
		}
		if report.Classification.TestDefinitionDefect {
			reasons = append(reasons, "test-definition-defect=true")
		}
		if report.Classification.ExecutionFailure {
			reasons = append(reasons, "execution-failure=true")
		}
		if report.Classification.ExpectationMismatch {
			reasons = append(reasons, "expectation-mismatch=true")
		}
		if len(reasons) > 0 {
			diagnostics = append(diagnostics, Diagnostic{Name: mutation.MutationID, Message: fmt.Sprintf("mutation=%q property=%q key=%q: %s", mutation.MutationID, report.PropertyID, mutation.PerturbationKey, strings.Join(reasons, "; "))})
		}
	}
	return diagnostics
}

// CheckMatrix integrates RunMatrix with Go tests. The batch has already run once
// before subtests are opened; subtests report the retained per-row comparisons
// and never rerun adapters or mutations.
func CheckMatrix[I, O, F, E any](t testing.TB, contract *Contract[I, O, F, E], rows []Row[I, O, F, E], options ...Option) Result {
	if nilTB(t) {
		return Result{Diagnostics: []Diagnostic{{Name: "matrix", Message: "svtest: nil testing.TB"}}}
	}
	t.Helper()
	result, err := RunMatrix(contract, rows, options...)
	if err != nil {
		t.Errorf("svtest matrix: %v", err)
		return result
	}
	diagnostics := make(map[string]string, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		diagnostics[diagnostic.Name] = diagnostic.Message
	}
	names := make([]string, 0, len(result.Batch.Mutations))
	for _, mutation := range result.Batch.Mutations {
		names = append(names, mutation.MutationID)
	}
	sort.Strings(names)
	if runner, ok := t.(interface {
		Run(string, func(*testing.T)) bool
	}); ok {
		for _, name := range names {
			name := name
			runner.Run(name, func(st *testing.T) {
				st.Helper()
				if message := diagnostics[name]; message != "" {
					st.Error(message)
				}
			})
		}
		return result
	}
	for _, diagnostic := range result.Diagnostics {
		t.Errorf("%s", diagnostic.Message)
	}
	return result
}

func nilTB(t testing.TB) bool {
	if t == nil {
		return true
	}
	v := reflect.ValueOf(t)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
