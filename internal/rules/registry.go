package rules

import (
	"strings"

	"github.com/jackchuka/mdschema/internal/parser"
	"github.com/jackchuka/mdschema/internal/schema"
	"github.com/jackchuka/mdschema/internal/vast"
)

// defaultConstructor builds the standard implementation of a check piece.
// Every check piece is registered exactly once here; both validation and
// template generation iterate this single registration, so a piece can never
// be checked without being generated (or vice versa).
type defaultConstructor func() Rule

// defaultConstructors is the sole registration list, in execution order.
func defaultConstructors() []defaultConstructor {
	return []defaultConstructor{
		func() Rule { return NewStructureRule() },
		func() Rule { return NewRequiredTextRule() },
		func() Rule { return NewForbiddenTextRule() },
		func() Rule { return NewCodeBlockRule() },
		func() Rule { return NewImageRule() },
		func() Rule { return NewTableRule() },
		func() Rule { return NewListRule() },
		func() Rule { return NewWordCountRule() },
		func() Rule { return NewParagraphRule() },
		func() Rule { return NewHeadingRule() },
		func() Rule { return NewLinkValidationRule() },
		func() Rule { return NewFrontmatterRule() },
	}
}

// Registry is the single shared set of check pieces. Validation iterates the
// pieces to collect violations; generation iterates the very same pieces and
// asks each one (through optional interfaces) whether it contributes template
// content. Pieces stay in their registered order and are addressed by name.
type Registry struct {
	pieces []Rule
}

// NewRegistry builds a registry containing the default implementations.
func NewRegistry() *Registry {
	constructors := defaultConstructors()
	pieces := make([]Rule, 0, len(constructors))
	for _, newPiece := range constructors {
		pieces = append(pieces, newPiece())
	}
	return &Registry{pieces: pieces}
}

// Names returns the registered check piece names in execution order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.pieces))
	for _, piece := range r.pieces {
		names = append(names, piece.Name())
	}
	return names
}

// activePieces returns the pieces with a name not present in disabled.
func (r *Registry) activePieces(disabled map[string]bool) []Rule {
	pieces := make([]Rule, 0, len(r.pieces))
	for _, piece := range r.pieces {
		if !disabled[piece.Name()] {
			pieces = append(pieces, piece)
		}
	}
	return pieces
}

// pieceByName returns the registered piece carrying the given name.
func (r *Registry) pieceByName(name string) (Rule, bool) {
	for _, piece := range r.pieces {
		if piece.Name() == name {
			return piece, true
		}
	}
	return nil, false
}

// replace swaps the registered piece carrying replacement.Name() for it.
// Order and position are preserved.
func (r *Registry) replace(replacement Rule) bool {
	for i, piece := range r.pieces {
		if piece.Name() == replacement.Name() {
			r.pieces[i] = replacement
			return true
		}
	}
	return false
}

// Validator runs registered check pieces against a document.
type Validator struct {
	registry *Registry
	disabled map[string]bool
}

// NewValidator creates a validator backed by a fresh default registry.
func NewValidator() *Validator {
	return NewValidatorWithRegistry(NewRegistry())
}

// NewValidatorWithRegistry creates a validator backed by the given registry.
// The same registry can be shared with a Generator so that validation and
// generation always see the exact same check pieces.
func NewValidatorWithRegistry(registry *Registry) *Validator {
	return &Validator{
		registry: registry,
		disabled: make(map[string]bool),
	}
}

// Disable turns off a check piece by name. Disabling one piece never changes
// the violations produced by the others, including their order. Returns false
// when no piece carries that name.
func (v *Validator) Disable(name string) bool {
	if _, ok := v.registry.pieceByName(name); !ok {
		return false
	}
	v.disabled[name] = true
	return true
}

// Enable turns a previously disabled check piece back on.
func (v *Validator) Enable(name string) bool {
	if _, ok := v.registry.pieceByName(name); !ok {
		return false
	}
	delete(v.disabled, name)
	return true
}

// Replace substitutes the implementation of a check piece, addressed by the
// replacement's Name(). Restoring the default implementation reproduces the
// original behavior byte for byte. Returns false for an unknown name.
func (v *Validator) Replace(replacement Rule) bool {
	return v.registry.replace(replacement)
}

// Validate runs all enabled pieces against a document with a specified root
// directory. The rootDir is used for resolving absolute paths (e.g., /path
// links).
func (v *Validator) Validate(doc *parser.Document, s *schema.Schema, rootDir string) []Violation {
	ctx := vast.NewContext(doc, s, rootDir)

	violations := make([]Violation, 0)
	for _, piece := range v.registry.activePieces(v.disabled) {
		violations = append(violations, piece.ValidateWithContext(ctx)...)
	}
	return violations
}

// ValidateRule runs a single check piece by name, independently of the
// others. Disabled state is ignored so the named piece can always be run on
// its own.
func (v *Validator) ValidateRule(name string, doc *parser.Document, s *schema.Schema, rootDir string) []Violation {
	piece, ok := v.registry.pieceByName(name)
	if !ok {
		return nil
	}
	ctx := vast.NewContext(doc, s, rootDir)
	return piece.ValidateWithContext(ctx)
}

// Generator creates markdown content by asking each registered check piece to
// contribute. It iterates the same registry as validation; no concrete piece
// kind (heading, link, table, ...) is branched on here.
type Generator struct {
	registry *Registry
	disabled map[string]bool
}

// NewGenerator creates a generator backed by a fresh default registry.
func NewGenerator() *Generator {
	return NewGeneratorWithRegistry(NewRegistry())
}

// NewGeneratorWithRegistry creates a generator backed by the given registry.
func NewGeneratorWithRegistry(registry *Registry) *Generator {
	return &Generator{
		registry: registry,
		disabled: make(map[string]bool),
	}
}

// Disable turns off content generation for a check piece by name.
func (g *Generator) Disable(name string) bool {
	if _, ok := g.registry.pieceByName(name); !ok {
		return false
	}
	g.disabled[name] = true
	return true
}

// GenerateContent generates content for an element by letting each enabled
// piece contribute. If no piece writes anything, the section keeps the
// standard TODO placeholder instead of becoming an empty section.
func (g *Generator) GenerateContent(builder *strings.Builder, element schema.StructureElement) {
	contentGenerated := false

	for _, piece := range g.registry.activePieces(g.disabled) {
		if structural, ok := piece.(StructuralRule); ok {
			if structural.GenerateContent(builder, element) {
				contentGenerated = true
			}
		}
	}

	if !contentGenerated {
		builder.WriteString("TODO: Add content for this section.\n\n")
	}
}

// GenerateDocument generates document-level content (e.g. frontmatter) by
// letting each enabled piece contribute.
func (g *Generator) GenerateDocument(builder *strings.Builder, s *schema.Schema) {
	for _, piece := range g.registry.activePieces(g.disabled) {
		if documentGenerator, ok := piece.(FrontmatterGenerator); ok {
			documentGenerator.Generate(builder, s)
		}
	}
}

// GenerateFrontmatter is retained as an alias of GenerateDocument.
func (g *Generator) GenerateFrontmatter(builder *strings.Builder, s *schema.Schema) {
	g.GenerateDocument(builder, s)
}
