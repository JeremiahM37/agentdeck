package memory

import "context"

// Brief reports empty knowledge separately from a failed or partial lookup.
type Brief struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Facts    []Fact `json:"facts"`
}

func LoadBrief(ctx context.Context, p Provider, project string) Brief {
	b := Brief{Provider: "none", Status: "disabled", Message: "Memory is not configured.", Facts: []Fact{}}
	if p == nil || p.Name() == "none" {
		return b
	}
	b.Provider = p.Name()
	facts, err := p.Recall(ctx, project, 8)
	if facts != nil {
		b.Facts = facts
	}
	switch {
	case err != nil && len(facts) > 0:
		b.Status, b.Message = "partial", "Memory is partially unavailable. Some context could not be retrieved; work can continue."
	case err != nil:
		b.Status, b.Message = "unavailable", "Memory unavailable. Work can continue using the repository and saved handoffs."
	case len(facts) == 0:
		b.Status, b.Message = "empty", "No relevant memory found for this project."
	default:
		b.Status, b.Message = "ready", "Project memory loaded."
	}
	return b
}

func (b Brief) Prompt() string {
	if b.Status == "disabled" {
		return ""
	}
	return "## Memory status\n" + b.Message + "\n\n" + Prime(b.Facts)
}
