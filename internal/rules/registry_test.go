package rules

import (
	"strings"
	"testing"

	"github.com/jackchuka/mdschema/internal/parser"
	"github.com/jackchuka/mdschema/internal/schema"
	"github.com/jackchuka/mdschema/internal/vast"
)

func parseDoc(t *testing.T, content string) *parser.Document {
	t.Helper()
	doc, err := parser.New().Parse("test.md", []byte(content))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func withoutRule(violations []Violation, name string) []Violation {
	out := make([]Violation, 0, len(violations))
	for _, v := range violations {
		if v.Rule != name {
			out = append(out, v)
		}
	}
	return out
}

// defaultCheckerNames records the canonical, order-sensitive registry. It is
// the one and only place tests enumerate the registered checkers.
var defaultCheckerNames = []string{
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

func TestDefaultRegistryIsSingleSource(t *testing.T) {
	got := DefaultRegistry().Names()
	if len(got) != len(defaultCheckerNames) {
		t.Fatalf("registered %d checkers, want %d: %v", len(got), len(defaultCheckerNames), got)
	}
	for i, want := range defaultCheckerNames {
		if got[i] != want {
			t.Fatalf("checker at position %d = %q, want %q (full list: %v)", i, got[i], want, got)
		}
	}

	// Validator and generator share the exact same registry-backed set.
	registry := DefaultRegistry()
	validatorNames := NewValidator(WithRegistry(registry)).Registry().Names()
	generatorNames := NewGenerator(WithRegistry(registry)).Registry().Names()
	if strings.Join(validatorNames, ",") != strings.Join(generatorNames, ",") {
		t.Fatalf("validator %v and generator %v must share one registry", validatorNames, generatorNames)
	}
}

func TestRunSingleCheckerByName(t *testing.T) {
	doc := parseDoc(t, "# Title\n")
	s := &schema.Schema{
		Structure: []schema.StructureElement{
			{Heading: schema.HeadingPattern{Pattern: "# Title"}},
			{Heading: schema.HeadingPattern{Pattern: "## Missing"}},
		},
	}

	onlyStructure := NewValidator(WithOnly("structure")).Validate(doc, s, "")
	if len(onlyStructure) == 0 {
		t.Fatal("running structure alone should report the missing section")
	}
	for _, v := range onlyStructure {
		if v.Rule != "structure" {
			t.Fatalf("single-checker run reported %q, want only structure", v.Rule)
		}
	}
}

func TestDisablingOneCheckerLeavesRestIdentical(t *testing.T) {
	doc := parseDoc(t, "# Title\n")
	s := &schema.Schema{
		Structure: []schema.StructureElement{
			{
				Heading: schema.HeadingPattern{Pattern: "# Title"},
				SectionRules: &schema.SectionRules{
					Paragraphs: &schema.ParagraphRule{Min: 2},
				},
			},
			{Heading: schema.HeadingPattern{Pattern: "## Missing"}},
		},
	}

	all := NewValidator().Validate(doc, s, "")

	// Independently derive what every non-structure checker would report.
	registry := DefaultRegistry().Disable("structure")
	rest := NewValidator(WithRegistry(registry)).Validate(doc, s, "")

	want := withoutRule(all, "structure")
	if len(rest) == 0 || len(rest) == len(all) {
		t.Fatalf("expected structure to contribute some violations; all=%d rest=%d", len(all), len(rest))
	}
	if len(rest) != len(want) {
		t.Fatalf("disabling structure changed other results: got %d, want %d", len(rest), len(want))
	}
	for i := range want {
		if rest[i] != want[i] {
			t.Fatalf("violation %d differs after disable:\n got  %+v\n want %+v", i, rest[i], want[i])
		}
	}
}

func TestOverrideThenRestoreIsByteIdentical(t *testing.T) {
	doc := parseDoc(t, "# Title\n")
	s := &schema.Schema{
		Structure: []schema.StructureElement{
			{Heading: schema.HeadingPattern{Pattern: "# Title"}},
			{Heading: schema.HeadingPattern{Pattern: "## Missing"}},
		},
	}

	baseline := NewValidator().Validate(doc, s, "")

	registry := DefaultRegistry()
	registry.Override("structure", func() Rule { return quietStructure{} })

	swapped := NewValidator(WithRegistry(registry)).Validate(doc, s, "")
	if len(swapped) >= len(baseline) {
		t.Fatalf("override should silence structure: baseline=%d swapped=%d", len(baseline), len(swapped))
	}

	// Restore default implementation; results must be verbatim identical.
	registry.Override("structure", nil)
	restored := NewValidator(WithRegistry(registry)).Validate(doc, s, "")
	if len(restored) != len(baseline) {
		t.Fatalf("restored produced %d violations, want %d", len(restored), len(baseline))
	}
	for i := range baseline {
		if restored[i] != baseline[i] {
			t.Fatalf("restored violation %d differs:\n got  %+v\n want %+v", i, restored[i], baseline[i])
		}
	}
}

// quietStructure reports the correct name but never emits violations.
type quietStructure struct{}

func (quietStructure) Name() string                                    { return "structure" }
func (quietStructure) ValidateWithContext(_ *vast.Context) []Violation { return nil }

func TestGeneratorKeepsPlaceholderWhenCheckerWritesNothing(t *testing.T) {
	// No checker generates content for a section with no section rules.
	s := &schema.Schema{
		Structure: []schema.StructureElement{
			{Heading: schema.HeadingPattern{Pattern: "# Title"}},
		},
	}

	out := generateSingleSection(t, NewGenerator(), s)
	if !strings.Contains(out, "TODO: Add content for this section.") {
		t.Fatalf("placeholder must remain when no checker writes content, got:\n%s", out)
	}

	// With a structural rule present, that checker supplies the content and the
	// placeholder is suppressed.
	s.Structure[0].SectionRules = &schema.SectionRules{
		Paragraphs: &schema.ParagraphRule{Min: 1},
	}
	out = generateSingleSection(t, NewGenerator(), s)
	if strings.Contains(out, "TODO: Add content for this section.") {
		t.Fatalf("placeholder should not appear when a checker writes content, got:\n%s", out)
	}
}

func TestFrontmatterRegisteredOnceAndReusedByGenerator(t *testing.T) {
	s := &schema.Schema{
		Frontmatter: &schema.FrontmatterConfig{
			Fields: []schema.FrontmatterField{{Name: "title", Type: "string"}},
		},
	}

	var builder strings.Builder
	NewGenerator().GenerateFrontmatter(&builder, s)
	out := builder.String()
	if !strings.Contains(out, "title") || !strings.HasPrefix(out, "---\n") {
		t.Fatalf("frontmatter generator should be discovered from the registry, got:\n%s", out)
	}

	// Disabling frontmatter in the shared registry silences generation too.
	registry := DefaultRegistry().Disable("frontmatter")
	var quiet strings.Builder
	NewGenerator(WithRegistry(registry)).GenerateFrontmatter(&quiet, s)
	if quiet.Len() != 0 {
		t.Fatalf("disabled frontmatter must not generate anything, got:\n%s", quiet.String())
	}
}

func generateSingleSection(t *testing.T, g *Generator, s *schema.Schema) string {
	t.Helper()
	var builder strings.Builder
	g.GenerateContent(&builder, s.Structure[0])
	return builder.String()
}
