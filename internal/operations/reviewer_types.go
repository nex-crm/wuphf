package operations

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReviewerHumanOnly is the sentinel DefaultReviewer / ReviewerPaths value
// that disables agent approval for a promotion. When the resolver returns
// this value, Lane C keeps the promotion in Pending until a human clicks
// Approve in the web UI.
const ReviewerHumanOnly = "human-only"

// ReviewerFallback is the final fallback when a blueprint declares no
// default_reviewer and no matching reviewer_paths entry.
const ReviewerFallback = "ceo"

// ReviewerPathRule is a single entry in a blueprint's reviewer_paths map,
// preserving declaration order so "first match wins" is deterministic.
type ReviewerPathRule struct {
	Pattern  string
	Reviewer string
}

// ReviewerPathMap is an order-preserving map of glob patterns to reviewer
// slugs. It unmarshals from a standard YAML mapping (`key: value`) and
// keeps pairs in declaration order so ResolveReviewer can match in the
// same order the blueprint author wrote them.
type ReviewerPathMap []ReviewerPathRule

// Len reports how many rules are configured.
func (m ReviewerPathMap) Len() int { return len(m) }

// AsMap flattens the ordered rules into a plain map[string]string for
// callers that only need lookup semantics (e.g. JSON responses).
func (m ReviewerPathMap) AsMap() map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for _, rule := range m {
		out[rule.Pattern] = rule.Reviewer
	}
	return out
}

// UnmarshalYAML decodes a YAML mapping node into a ReviewerPathMap while
// preserving the declaration order of its keys.
func (m *ReviewerPathMap) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		*m = nil
		return nil
	}
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node.Kind != yaml.MappingNode {
		if node.Kind == yaml.ScalarNode && strings.TrimSpace(node.Value) == "" {
			*m = nil
			return nil
		}
		return fmt.Errorf("reviewer_paths: expected mapping, got %v", node.Kind)
	}
	out := make(ReviewerPathMap, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("reviewer_paths: key must be a scalar string, got %v", keyNode.Kind)
		}
		if valNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("reviewer_paths: value for %q must be a scalar string, got %v", keyNode.Value, valNode.Kind)
		}
		out = append(out, ReviewerPathRule{
			Pattern:  strings.TrimSpace(keyNode.Value),
			Reviewer: strings.TrimSpace(valNode.Value),
		})
	}
	*m = out
	return nil
}

// MarshalYAML emits the rules as a standard YAML mapping so round-trips
// look identical to hand-authored blueprints.
func (m ReviewerPathMap) MarshalYAML() (any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, rule := range m {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: rule.Pattern},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: rule.Reviewer},
		)
	}
	return node, nil
}
