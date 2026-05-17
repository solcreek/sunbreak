package matcher

import (
	"errors"
	"regexp"
	"strings"

	"radar/internal/model"
)

type Matcher struct {
	rules []compiledRule
}

type compiledRule struct {
	rule  model.Rule
	regex *regexp.Regexp
	term  string
}

type Result struct {
	Rule        model.Rule
	MatchedText string
	Score       float64
}

func New(rules []model.Rule) (*Matcher, error) {
	compiled := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			return nil, errors.New("rule pattern is required: " + rule.Name)
		}
		switch rule.Type {
		case "keyword", "":
			term := rule.Pattern
			if !rule.CaseSensitive {
				term = strings.ToLower(term)
			}
			compiled = append(compiled, compiledRule{rule: rule, term: term})
		case "regex":
			pattern := rule.Pattern
			if !rule.CaseSensitive {
				pattern = "(?i)" + pattern
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, err
			}
			compiled = append(compiled, compiledRule{rule: rule, regex: re})
		default:
			return nil, errors.New("unsupported rule type: " + rule.Type)
		}
	}
	return &Matcher{rules: compiled}, nil
}

func (m *Matcher) Match(item model.Item) []Result {
	text := item.Title + "\n" + item.Content + "\n" + item.URL
	var out []Result
	for _, rule := range m.rules {
		if rule.regex != nil {
			found := rule.regex.FindString(text)
			if found != "" {
				out = append(out, Result{Rule: rule.rule, MatchedText: found, Score: 1})
			}
			continue
		}
		searchText := text
		if !rule.rule.CaseSensitive {
			searchText = strings.ToLower(searchText)
		}
		if strings.Contains(searchText, rule.term) {
			out = append(out, Result{Rule: rule.rule, MatchedText: rule.rule.Pattern, Score: 1})
		}
	}
	return out
}
