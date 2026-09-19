package rules

// Registry is the single source of truth for every registered checker. Both
// validation and template generation derive their checkers exclusively from a
// Registry; there is no kind-specific (heading/link/paragraph/...) branch in
// the callers. Each entry is addressed by name so that an individual checker
// can be selected, disabled, or replaced without touching the entry point.
type Registry struct {
	factories []func() Rule
	byname    map[string]int
	overrides map[string]func() Rule
	disabled  map[string]bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byname:    make(map[string]int),
		overrides: make(map[string]func() Rule),
		disabled:  make(map[string]bool),
	}
}

// DefaultRegistry returns a registry pre-populated with the standard checker
// set, in the exact order validation has always run them.
func DefaultRegistry() *Registry {
	return NewRegistry().
		Register(asRule(NewStructureRule)).
		Register(asRule(NewRequiredTextRule)).
		Register(asRule(NewForbiddenTextRule)).
		Register(asRule(NewCodeBlockRule)).
		Register(asRule(NewImageRule)).
		Register(asRule(NewTableRule)).
		Register(asRule(NewListRule)).
		Register(asRule(NewWordCountRule)).
		Register(asRule(NewParagraphRule)).
		Register(asRule(NewHeadingRule)).
		Register(asRule(NewLinkValidationRule)).
		Register(asRule(NewFrontmatterRule))
}

// asRule adapts a concrete checker constructor to the Rule-returning factory
// signature the registry stores.
func asRule[T Rule](factory func() T) func() Rule {
	return func() Rule { return factory() }
}

// Register appends a checker factory under the name reported by an instance it
// builds. Re-registering an existing name replaces its factory while preserving
// its original position. It returns the registry for chaining.
func (r *Registry) Register(factory func() Rule) *Registry {
	name := factory().Name()
	if idx, ok := r.byname[name]; ok {
		r.factories[idx] = factory
		return r
	}
	r.byname[name] = len(r.factories)
	r.factories = append(r.factories, factory)
	return r
}

// Override substitutes a different implementation for a named checker without
// changing registration order or the default entry point. The factory must
// report the same name. Restoring the original implementation is done by
// passing nil, after which output is byte-identical to the default build.
func (r *Registry) Override(name string, factory func() Rule) *Registry {
	if _, ok := r.byname[name]; !ok {
		return r
	}
	if factory == nil {
		delete(r.overrides, name)
		return r
	}
	r.overrides[name] = factory
	return r
}

// Disable turns a named checker off. Disabling one checker never alters the
// violations or generated content contributed by any other checker, including
// their relative order.
func (r *Registry) Disable(names ...string) *Registry {
	for _, name := range names {
		r.disabled[name] = true
	}
	return r
}

// Enable turns a previously disabled named checker back on.
func (r *Registry) Enable(names ...string) *Registry {
	for _, name := range names {
		delete(r.disabled, name)
	}
	return r
}

// Has reports whether a checker is registered under name.
func (r *Registry) Has(name string) bool {
	_, ok := r.byname[name]
	return ok
}

// Names returns all registered checker names in registration order, regardless
// of whether each one is enabled or overridden.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.factories))
	for _, factory := range r.factories {
		names = append(names, factory().Name())
	}
	return names
}

// build instantiates the enabled checkers in registration order, applying any
// overrides. It is the only place that turns the registry into rule instances.
func (r *Registry) build() []Rule {
	built := make([]Rule, 0, len(r.factories))
	for _, factory := range r.factories {
		name := factory().Name()
		if r.disabled[name] {
			continue
		}
		built = append(built, r.instantiate(name, factory))
	}
	return built
}

// buildNamed instantiates only the requested enabled checkers, returned in
// global registration order so a single checker can be run in isolation.
func (r *Registry) buildNamed(names []string) []Rule {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	built := make([]Rule, 0, len(names))
	for _, factory := range r.factories {
		name := factory().Name()
		if !wanted[name] || r.disabled[name] {
			continue
		}
		built = append(built, r.instantiate(name, factory))
	}
	return built
}

// instantiate applies an override for name while guaranteeing the resulting
// checker still reports that name; a misnamed override falls back to default.
func (r *Registry) instantiate(name string, factory func() Rule) Rule {
	active := factory
	if override, ok := r.overrides[name]; ok {
		active = override
	}
	rule := active()
	if rule.Name() != name {
		return factory()
	}
	return rule
}
