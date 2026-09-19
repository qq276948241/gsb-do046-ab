package rules

import (
	"strings"

	"github.com/jackchuka/mdschema/internal/schema"
	"github.com/jackchuka/mdschema/internal/vast"
)

// Rule is the base interface for every check piece. A check piece validates a
// document and may optionally contribute generated template content. Each
// piece is addressed by the string returned by Name, so it can be run,
// disabled, or replaced on its own without touching the shared entry point.
type Rule interface {
	// Name returns the rule identifier
	Name() string

	// ValidateWithContext uses pre-established section-schema mappings via VAST
	ValidateWithContext(ctx *vast.Context) []Violation
}

// StructuralRule is implemented by check pieces that generate content for a
// structure element (section) during template generation.
type StructuralRule interface {
	Rule

	// GenerateContent generates markdown content for elements that match this rule
	// Returns true if the rule handled content generation for this element
	GenerateContent(builder *strings.Builder, element schema.StructureElement) bool
}

// FrontmatterGenerator is implemented by check pieces that generate
// document-level content (currently frontmatter) during template generation.
type FrontmatterGenerator interface {
	Generate(builder *strings.Builder, s *schema.Schema) bool
}
