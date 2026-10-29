package control

// The operations on scenario templates. list and show read without an admin session; derive stores a new
// custom template and needs one.
const (
	OpTemplatesList   = "templates.list"
	OpTemplatesShow   = "templates.show"
	OpTemplatesDerive = "templates.derive"
)

// The origins of a template and the sources they come from.
const (
	TemplateBuiltin = "builtin"
	TemplateCustom  = "custom"

	// TemplateSourceEmbedded is the source of a builtin template, TemplateSourceConfig of a custom one.
	TemplateSourceEmbedded = "embedded"
	TemplateSourceConfig   = "config"
)

// TemplateSummary is one template in OpTemplatesList.
type TemplateSummary struct {
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
	Steps       int    `json:"steps"`
}

// TemplatesListResult is the answer to OpTemplatesList: builtin templates first, then custom ones, each
// group by name.
type TemplatesListResult struct {
	Templates []TemplateSummary `json:"templates"`
}

// TemplateRef names one template for OpTemplatesShow.
type TemplateRef struct {
	Name string `json:"name"`
}

// TemplateDetail is the answer to OpTemplatesShow. File is the template as YAML text. Templates hold
// placeholders and no credential values.
type TemplateDetail struct {
	TemplateSummary
	File string `json:"file"`
}

// TemplateDeriveParams are the parameters of OpTemplatesDerive: a new custom template Name with the steps
// of the template From. Description, when set, replaces the description of From. The call fails with
// CodeExists for a taken name, CodeNotFound for an unknown From and CodeInvalid with problems for a name or
// content that fails the validation.
type TemplateDeriveParams struct {
	From        string `json:"from"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
