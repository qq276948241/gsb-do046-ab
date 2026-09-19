package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackchuka/mdschema/internal/parser"
	"github.com/jackchuka/mdschema/internal/schema"
	"github.com/jackchuka/mdschema/internal/vast"
)

// ruleNames is the canonical, order-sensitive registration. Validation and
// generation must both consume exactly this list and nothing else.
var ruleNames = []string{
	"structure",
	"required-text",
	"forbidden-text",
	"codeblock",
	"image",
	"table",
	"list",
	"word-count",
	"paragraph",
	"heading",
	"link",
	"frontmatter",
}

func writeTempSchema(t *testing.T, yamlContent string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".mdschema.yml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("writing temp schema: %v", err)
}
	return path
}

func loadDoc(t *testing.T, markdown string) *parser.Document {
	t.Helper()
	doc, err := parser.New().Parse("doc.md", []byte(markdown))
	if err != nil {
		t.Fatalf("parsing document: %v", err)
	}
	return doc
}

func ruleSet(violations []Violation) map[string]bool {
	set := make(map[string]bool)
	for _, v := range violations {
		set[v.Rule] = true
	}
	return set
}

// TestRegistryIsSingleSource ensures there is exactly one ordered list and
// that every piece is reachable by name.
func TestRegistryIsSingleSource(t *testing.T) {
	registry := NewRegistry()

	if got := registry.Names(); !equalStrings(got, ruleNames) {
		t.Fatalf("registry names = %v, want %v", got, ruleNames)
	}

	for _, name := range ruleNames {
		if _, ok := registry.pieceByName(name); !ok {
			t.Errorf("piece %q not registered", name)
		}
	}
}

// TestDisableIsolatesOthers verifies that turning one piece off removes only
// its own violations: the remaining messages, line numbers, and order stay
// byte-for-byte identical.
func TestDisableIsolatesOthers(t *testing.T) {
	schemaPath := writeTempSchema(t, `
structure:
  - heading: "# Title"
    optional: true
    children:
      - heading: "## Installation"
        code_blocks:
          - { lang: bash, min: 1 }
`)
	s, _, err := schema.Load(schemaPath)
	if err != nil {
		t.Fatalf("loading schema: %v", err)
	}

	// "# Wrong" trips the structure rule (unexpected section); the missing
	// bash block trips the codeblock rule.
	doc := loadDoc(t, "# Wrong\n\n## Installation\n\nno code here\n")

	all := NewValidator().Validate(doc, s, "")
	if len(ruleSet(all)) < 2 {
		t.Fatalf("expected violations from multiple pieces, got %v", ruleSet(all))
	}

	for _, disabled := range ruleNames {
		t.Run(disabled, func(t *testing.T) {
			validator := NewValidator()
			if !validator.Disable(disabled) {
				t.Fatalf("Disable(%q) returned false", disabled)
			}

			got := validator.Validate(doc, s, "")

			want := make([]Violation, 0, len(all))
			for _, v := range all {
				if v.Rule != disabled {
					want = append(want, v)
				}
			}

			if !equalViolations(got, want) {
				t.Fatalf("disabling %q changed the other pieces' output:\n got %#v\nwant %#v", disabled, got, want)
			}
			if ruleSet(got)[disabled] {
				t.Fatalf("disabled piece %q still reported violations", disabled)
			}
		})
	}
}

// TestDisableUnknownName reports failure without changing behavior.
func TestDisableUnknownName(t *testing.T) {
	validator := NewValidator()
	if validator.Disable("does-not-exist") {
		t.Fatal("Disable returned true for an unknown piece")
	}
	if validator.Enable("does-not-exist") {
		t.Fatal("Enable returned true for an unknown piece")
	}
}

// TestRunSinglePiece runs one named piece in isolation, even while disabled.
func TestRunSinglePiece(t *testing.T) {
	// Document trips both structure and link rules.
	schemaPath := writeTempSchema(t, `
links:
  validate_internal: true
structure:
  - heading: "# Title"
    optional: true
`)
	s, _, err := schema.Load(schemaPath)
	if err != nil {
		t.Fatalf("loading schema: %v", err)
	}
	doc := loadDoc(t, "# Guide\n\n[broken](#missing-anchor)\n")

	validator := NewValidator()

	linkOnly := validator.ValidateRule("link", doc, s, "")
	if len(linkOnly) == 0 {
		t.Fatal("expected link violations when running the link piece alone")
	}
	for _, v := range linkOnly {
		if v.Rule != "link" {
			t.Fatalf("single-piece run reported %q violations", v.Rule)
		}
	}

	// Disabling the piece does not affect the explicit single-piece run.
	validator.Disable("link")
	if got := validator.ValidateRule("link", doc, s, ""); len(got) != len(linkOnly) {
		t.Fatalf("disabled single-piece run changed: got %d, want %d", len(got), len(linkOnly))
	}

	if validator.ValidateRule("does-not-exist", doc, s, "") != nil {
		t.Fatal("unknown piece should yield no violations")
	}
}

// alternateLinkRule is a drop-in replacement for the link piece that reports
// one deterministic violation.
type alternateLinkRule struct{}

func (alternateLinkRule) Name() string { return "link" }
func (alternateLinkRule) ValidateWithContext(ctx *vast.Context) []Violation {
	return []Violation{NewViolation("link", "alternate implementation", 42, 7)}
}

// TestReplaceAndRestore swaps an implementation without touching the entry
// point, then restores the default and demands byte-identical behavior.
func TestReplaceAndRestore(t *testing.T) {
	schemaPath := writeTempSchema(t, `
links:
  validate_internal: true
structure:
  - heading: "# Title"
    optional: true
`)
	s, _, err := schema.Load(schemaPath)
	if err != nil {
		t.Fatalf("loading schema: %v", err)
	}
	doc := loadDoc(t, "# Guide\n\n[broken](#missing-anchor)\n")

	registry := NewRegistry()
	original := NewValidatorWithRegistry(registry).Validate(doc, s, "")

	validator := NewValidatorWithRegistry(registry)

	// Swap in the alternate implementation.
	if !validator.Replace(alternateLinkRule{}) {
		t.Fatal("Replace returned false for a known piece name")
	}

	swapped := validator.Validate(doc, s, "")
	var foundAlternate bool
	for _, v := range swapped {
		if v.Rule == "link" {
			if v.Message != "alternate implementation" || v.Line != 42 || v.Column != 7 {
				t.Fatalf("alternate implementation not used: %#v", v)
			}
			foundAlternate = true
		}
	}
	if !foundAlternate {
		t.Fatal("replacement piece never ran")
	}

	// Replacing an unknown name fails without changing anything.
	if validator.Replace(unknownNameRule{}) {
		t.Fatal("Replace returned true for an unknown piece name")
	}

	// Restore the default implementation; errors must be identical verbatim.
	defaultLink, ok := NewRegistry().pieceByName("link")
	if !ok {
		t.Fatal("default link piece missing from fresh registry")
	}
	if !validator.Replace(defaultLink) {
		t.Fatal("restoring default piece failed")
	}

	restored := validator.Validate(doc, s, "")
	if !equalViolations(restored, original) {
		t.Fatalf("restored output differs:\n got %#v\nwant %#v", restored, original)
	}
}

type unknownNameRule struct{}

func (unknownNameRule) Name() string                                        { return "does-not-exist" }
func (unknownNameRule) ValidateWithContext(ctx *vast.Context) []Violation { return nil }

// TestSharedRegistryValidatesAndGenerates proves validation and generation
// iterate the same registration: replacing a structural piece affects both.
func TestSharedRegistryValidatesAndGenerates(t *testing.T) {
	registry := NewRegistry()

	// Replace every structural content generator with a piece that never
	// generates, so sections fall back to the TODO placeholder.
	original := make([]Rule, len(ruleNames))
	for i, name := range ruleNames {
		piece, _ := registry.pieceByName(name)
		original[i] = piece
	}

	schemaPath := writeTempSchema(t, `
structure:
  - heading: "# Title"
    code_blocks:
      - { lang: go, min: 1 }
`)
	s, _, err := schema.Load(schemaPath)
	if err != nil {
		t.Fatalf("loading schema: %v", err)
	}

	gen := NewGeneratorWithRegistry(registry)
	var defaultBuilder strings.Builder
	gen.GenerateContent(&defaultBuilder, s.Structure[0])
	if strings.Contains(defaultBuilder.String(), "TODO: Add content for this section.") {
		t.Fatal("codeblock piece should have generated content from the default registry")
	}

	// Swap the codeblock piece for a check-only replacement.
	validator := NewValidatorWithRegistry(registry)
	if !validator.Replace(checkOnlyCodeBlock{}) {
		t.Fatal("replacing codeblock piece failed")
	}

	var swappedBuilder strings.Builder
	gen.GenerateContent(&swappedBuilder, s.Structure[0])
	if !strings.Contains(swappedBuilder.String(), "TODO: Add content for this section.") {
		t.Fatalf("expected TODO placeholder when no piece generates, got:\n%s", swappedBuilder.String())
	}

	// Restore all defaults; generated text must match exactly.
	for _, piece := range original {
		if !validator.Replace(piece) {
			t.Fatalf("restoring %q failed", piece.Name())
		}
	}
	var restoredBuilder strings.Builder
	gen.GenerateContent(&restoredBuilder, s.Structure[0])
	if restoredBuilder.String() != defaultBuilder.String() {
		t.Fatalf("restored generation differs:\n got %q\nwant %q", restoredBuilder.String(), defaultBuilder.String())
	}
}

// checkOnlyCodeBlock validates but contributes no generated content.
type checkOnlyCodeBlock struct{}

func (checkOnlyCodeBlock) Name() string                                        { return "codeblock" }
func (checkOnlyCodeBlock) ValidateWithContext(ctx *vast.Context) []Violation { return nil }

// TestNoPieceGeneratesKeepsPlaceholder covers the element with no section
// rules: every piece declines, and the standard placeholder must survive.
func TestNoPieceGeneratesKeepsPlaceholder(t *testing.T) {
	schemaPath := writeTempSchema(t, `
structure:
  - heading: "# Plain"
`)
	s, _, err := schema.Load(schemaPath)
	if err != nil {
		t.Fatalf("loading schema: %v", err)
	}

	gen := NewGenerator()
	var builder strings.Builder
	gen.GenerateContent(&builder, s.Structure[0])

	if builder.String() != "TODO: Add content for this section.\n\n" {
		t.Fatalf("placeholder changed or missing: %q", builder.String())
	}
}

// TestGenerationOrderFollowsRegistration ensures generation never reorders
// pieces relative to the shared registration.
func TestGenerationOrderFollowsRegistration(t *testing.T) {
	registry := NewRegistry()
	got := registry.Names()

	validator := NewValidatorWithRegistry(registry)
	generator := NewGeneratorWithRegistry(registry)

	if !equalStrings(generator.registry.Names(), got) {
		t.Fatal("generator registry diverged from registration order")
	}
	if !equalStrings(validator.registry.Names(), got) {
		t.Fatal("validator registry diverged from registration order")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalViolations(a, b []Violation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
