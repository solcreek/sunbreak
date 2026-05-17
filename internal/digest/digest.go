package digest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"sunbreak/internal/model"
)

func Build(matches []model.Match, windowStart, windowEnd time.Time) model.Digest {
	byRule := map[string][]model.Match{}
	for _, match := range matches {
		byRule[match.Rule.Name] = append(byRule[match.Rule.Name], match)
	}
	ruleNames := make([]string, 0, len(byRule))
	for name := range byRule {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)
	var body strings.Builder
	body.WriteString(fmt.Sprintf("Window: %s to %s\n", windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339)))
	body.WriteString(fmt.Sprintf("Matches: %d\n\n", len(matches)))
	for _, name := range ruleNames {
		items := byRule[name]
		body.WriteString(fmt.Sprintf("Rule: %s (%d)\n", name, len(items)))
		limit := len(items)
		if limit > 10 {
			limit = 10
		}
		for i := 0; i < limit; i++ {
			match := items[i]
			title := strings.TrimSpace(match.Item.Title)
			if title == "" {
				title = strings.TrimSpace(match.Item.Content)
			}
			if len(title) > 120 {
				title = title[:120] + "..."
			}
			body.WriteString(fmt.Sprintf("- [%s] %s", match.Item.SourceName, title))
			if match.Item.URL != "" {
				body.WriteString(" - " + match.Item.URL)
			}
			body.WriteString("\n")
		}
		if len(items) > limit {
			body.WriteString(fmt.Sprintf("- ... and %d more\n", len(items)-limit))
		}
		body.WriteString("\n")
	}
	subject := fmt.Sprintf("Sunbreak digest: %d matches", len(matches))
	if len(matches) == 0 {
		subject = "Sunbreak digest: no matches"
	}
	return model.Digest{
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Subject:     subject,
		Body:        body.String(),
		CreatedAt:   time.Now().UTC(),
	}
}
