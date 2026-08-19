package svg

import (
	"regexp"
	"strings"
)

var (
	styleRegex     = regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`)
	ruleRegex      = regexp.MustCompile(`\.([\w-]+)\s*\{\s*([^}]+)\s*\}`)
	classAttrRegex = regexp.MustCompile(`class="([^"]+)"`)
)

// Inliner processes an SVG string and inlines CSS class styles into element attributes.
// This is primarily to support flutter_svg which has limited CSS support.
type Inliner struct{}

// NewInliner creates a new SVG inliner.
func NewInliner() *Inliner {
	return &Inliner{}
}

// InlineStyles transforms an SVG string by moving styles from <style> blocks into inline attributes.
func (i *Inliner) InlineStyles(svg string) string {
	// 1. Extract rules from <style> blocks
	styleMatch := styleRegex.FindStringSubmatch(svg)
	if len(styleMatch) < 2 {
		return svg
	}

	styles := styleMatch[1]
	rules := make(map[string]string)

	// Match patterns like: .cls-1 { fill: #eadfc7; }
	matches := ruleRegex.FindAllStringSubmatch(styles, -1)
	for _, match := range matches {
		if len(match) == 3 {
			className := match[1]
			body := strings.TrimSpace(match[2])
			rules[className] = body
		}
	}

	// 2. Remove style blocks
	processed := styleRegex.ReplaceAllString(svg, "")

	// 3. Replace class="className" with properties from style body
	processed = classAttrRegex.ReplaceAllStringFunc(processed, func(match string) string {
		submatch := classAttrRegex.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}

		classNames := strings.Fields(submatch[1])
		var combinedStyles []string
		for _, c := range classNames {
			if style, ok := rules[c]; ok {
				combinedStyles = append(combinedStyles, style)
			}
		}

		if len(combinedStyles) == 0 {
			return match
		}

		// Convert "fill: #eadfc7; stroke: #000;" to individual attributes
		allStyles := strings.Join(combinedStyles, ";")
		parts := strings.Split(allStyles, ";")
		var attributes []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			kv := strings.SplitN(p, ":", 2)
			if len(kv) == 2 {
				name := strings.TrimSpace(kv[0])
				value := strings.TrimSpace(kv[1])
				attributes = append(attributes, name+"=\""+value+"\"")
			}
		}

		return strings.Join(attributes, " ")
	})

	return processed
}
