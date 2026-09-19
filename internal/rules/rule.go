package rules

import (
	"strings"

	"github.com/jackchuka/mdschema/internal/parser"
	"github.com/jackchuka/mdschema/internal/schema"
	"github.com/jackchuka/mdschema/internal/vast"
)

// Rule is the base interface for all validation rules
type Rule interface {
	// Name returns the rule identifier
	Name() string

	// ValidateWithContext uses pre-established section-schema mappings via VAST
	ValidateWithContext(ctx *vast.Context) []Violation
}

// StructuralRule validates and generates content for structure elements (sections)
type StructuralRule interface {
	Rule

	// GenerateContent generates markdown content for elements that match this rule
	// Returns true if the rule handled content generation for this element
	GenerateContent(builder *strings.Builder, element schema.StructureElement) bool
}

// FrontmatterGenerator generates document-level frontmatter content
type FrontmatterGenerator interface {
	Generate(builder *strings.Builder, s *schema.Schema) bool
}

// option is the shared configuration for both the Validator and the Generator.
type option struct {
	registry *Registry
	only     map[string]bool
}

// Option customizes which checkers a Validator or Generator uses.
type Option func(*option)

// WithRegistry supplies the single shared checker registry. Both checking and
// generation consume the same registry instance so a checker is registered
// exactly once.
func WithRegistry(registry *Registry) Option {
	return func(o *option) {
		o.registry = registry
	}
}

// WithOnly restricts a run to the named checkers, addressed by their Name().
// They still execute in global registration order. The main entry point is
// unchanged; this simply selects a subset (or a single checker in isolation).
func WithOnly(names ...string) Option {
	return func(o *option) {
		o.only = make(map[string]bool, len(names))
		for _, name := range names {
			o.only[name] = true
		}
	}
}

func defaultOption() option {
	return option{registry: DefaultRegistry()}
}

func (o option) checkers() []Rule {
	if o.only != nil {
		names := make([]string, 0, len(o.only))
		for name := range o.only {
			names = append(names, name)
		}
		return o.registry.buildNamed(names)
	}
	return o.registry.build()
}

// Validator manages and runs the checkers obtained from a registry.
type Validator struct {
	option
}

// Registry returns the registry this validator draws its checkers from.
func (v *Validator) Registry() *Registry { return v.option.registry }

// NewValidator creates a validator backed by the default shared registry.
// Pass WithRegistry and/or WithOnly to run a different implementation or just
// one named checker without modifying this entry point.
func NewValidator(opts ...Option) *Validator {
	o := defaultOption()
	for _, opt := range opts {
		opt(&o)
	}
	return &Validator{option: o}
}

// Validate runs all rules against a document with a specified root directory.
// The rootDir is used for resolving absolute paths (e.g., /path links).
func (v *Validator) Validate(doc *parser.Document, s *schema.Schema, rootDir string) []Violation {
	violations := make([]Violation, 0)

	// Create validation context with VAST
	ctx := vast.NewContext(doc, s, rootDir)

	for _, rule := range v.checkers() {
		ruleViolations := rule.ValidateWithContext(ctx)
		violations = append(violations, ruleViolations...)
	}

	return violations
}

// Generator creates markdown content using the same registered checkers as the
// Validator. It discovers capabilities by interface rather than by branching on
// concrete checker kinds, so registering a checker once is enough for both
// checking and generation.
type Generator struct {
	option
}

// Registry returns the registry this generator draws its checkers from.
func (g *Generator) Registry() *Registry { return g.option.registry }

// NewGenerator creates a template generator backed by the default shared
// registry. As with NewValidator, WithRegistry/WithOnly can swap in another
// implementation or select individual checkers.
func NewGenerator(opts ...Option) *Generator {
	o := defaultOption()
	for _, opt := range opts {
		opt(&o)
	}
	return &Generator{option: o}
}

// GenerateContent generates content for an element using all applicable rules
func (g *Generator) GenerateContent(builder *strings.Builder, element schema.StructureElement) {
	contentGenerated := false

	for _, checker := range g.checkers() {
		rule, ok := checker.(StructuralRule)
		if !ok {
			continue
		}
		before := builder.Len()
		rule.GenerateContent(builder, element)
		if builder.Len() > before {
			// A checker only counts as supplying this section when it actually
			// wrote bytes; a "handled" but empty result still leaves the
			// default TODO placeholder intact.
			contentGenerated = true
		}
	}

	// If no rule generated content, add default placeholder
	if !contentGenerated {
		builder.WriteString("TODO: Add content for this section.\n\n")
	}
}

// GenerateFrontmatter generates document frontmatter using the frontmatter generator
func (g *Generator) GenerateFrontmatter(builder *strings.Builder, s *schema.Schema) {
	for _, checker := range g.checkers() {
		if generator, ok := checker.(FrontmatterGenerator); ok {
			generator.Generate(builder, s)
			return
		}
	}
}
