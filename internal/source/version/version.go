// Package version selects the target version of a source from a list of tags. It is pure: it does no I/O and
// serves registry tags and git tags alike.
//
// Rules:
//   - A candidate is a complete SemVer tag, MAJOR.MINOR.PATCH with an optional leading "v". Incomplete
//     versions such as "1.2" or "16" and everything else that is not SemVer are ignored.
//   - Text after the first "-" is the variant ("apache", "alpine3.19"). Only tags with exactly the variant of
//     the current tag are candidates. A suffix whose first dot- or dash-separated part is a number or one of
//     alpha, beta, rc, pre, preview, dev, snapshot, nightly, canary, next, test or milestone (optionally
//     followed by digits) is a SemVer pre-release, not a variant, and is never offered.
//   - The optional tag filter is an RE2 expression applied unanchored to the whole candidate tag.
package version

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// Problem codes.
const (
	CodeFloatingTag      = "floating-tag"
	CodePrereleaseTag    = "prerelease-current"
	CodeInvalidTagFilter = "invalid-tag-filter"
	CodeInvalidRule      = "invalid-rule"
)

// Problem is a finding that prevents or limits a selection, with a hint for the operator.
type Problem struct {
	Code    string
	Message string
	Hint    string
}

// Selection is the result for one source.
type Selection struct {
	// Current is the tag the source is on, as given.
	Current string
	// Available holds every candidate tag with a higher version than Current, lowest first, regardless of the
	// update cap.
	Available []string
	// Target is the tag to move to under the version rule; empty when there is none.
	Target   string
	Problems []Problem
}

type parsedTag struct {
	tag                 string
	prefix              bool
	major, minor, patch uint64
	variant             string
}

type parseKind int

const (
	kindInvalid parseKind = iota
	kindPrerelease
	kindOK
)

var (
	tagRe = regexp.MustCompile(`^(v?)(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z][0-9A-Za-z.-]*))?$`)
	preRe = regexp.MustCompile(`(?i)^(alpha|beta|rc|pre|preview|dev|snapshot|nightly|canary|next|test|milestone)[0-9]*$`)
	numRe = regexp.MustCompile(`^[0-9]+$`)
)

func parseTag(tag string) (parsedTag, parseKind) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return parsedTag{}, kindInvalid
	}
	p := parsedTag{tag: tag, prefix: m[1] == "v", variant: m[5]}
	var err error
	for i, dst := range []*uint64{&p.major, &p.minor, &p.patch} {
		if *dst, err = strconv.ParseUint(m[2+i], 10, 63); err != nil {
			return parsedTag{}, kindInvalid
		}
	}
	if p.variant != "" {
		first, _, _ := strings.Cut(p.variant, ".")
		first, _, _ = strings.Cut(first, "-")
		if numRe.MatchString(first) || preRe.MatchString(first) {
			return p, kindPrerelease
		}
	}
	return p, kindOK
}

func (a parsedTag) less(b parsedTag) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

func (a parsedTag) sameVersion(b parsedTag) bool { return !a.less(b) && !b.less(a) }

// Select picks the available versions and the target for current from tags under rule.
func Select(current string, tags []string, rule config.VersionRule) Selection {
	sel := Selection{Current: current}
	cur, kind := parseTag(current)
	switch kind {
	case kindInvalid:
		sel.Problems = append(sel.Problems, Problem{
			Code:    CodeFloatingTag,
			Message: fmt.Sprintf("current tag %q is not a complete version, so newer versions cannot be determined", current),
			Hint:    "Pin the source to a full version such as 1.2.3, optionally with a variant suffix such as 1.2.3-alpine.",
		})
		return sel
	case kindPrerelease:
		sel.Problems = append(sel.Problems, Problem{
			Code:    CodePrereleaseTag,
			Message: fmt.Sprintf("current tag %q is a pre-release, so newer versions cannot be determined", current),
			Hint:    "Pin the source to a released version such as 1.2.3.",
		})
		return sel
	}

	maxUpdate, steps := rule.MaxUpdate, rule.MajorSteps
	if maxUpdate == "" {
		maxUpdate = config.LevelMajor
	}
	if steps == "" {
		steps = config.MajorDirect
	}
	ruleOK := true
	if maxUpdate != config.LevelPatch && maxUpdate != config.LevelMinor && maxUpdate != config.LevelMajor ||
		steps != config.MajorDirect && steps != config.MajorStep {
		ruleOK = false
		sel.Problems = append(sel.Problems, Problem{
			Code:    CodeInvalidRule,
			Message: fmt.Sprintf("unsupported version rule: max_update %q, major_steps %q", rule.MaxUpdate, rule.MajorSteps),
			Hint:    "Use max_update patch, minor or major and major_steps direct or stepwise.",
		})
	}
	var filter *regexp.Regexp
	if rule.TagFilter != "" {
		var err error
		if filter, err = regexp.Compile(rule.TagFilter); err != nil {
			sel.Problems = append(sel.Problems, Problem{
				Code:    CodeInvalidTagFilter,
				Message: "tag filter is not a valid RE2 expression",
				Hint:    "Correct the tag_filter expression of the version rule.",
			})
			return sel
		}
	}

	higher := candidates(cur, tags, filter)
	for _, c := range higher {
		sel.Available = append(sel.Available, c.tag)
	}
	if ruleOK {
		sel.Target = target(cur, higher, maxUpdate, steps)
	}
	return sel
}

// List returns the release tags of tags under the rule's tag filter, lowest version first, for a source whose
// current version is unknown. All variants are listed and no target is chosen.
func List(tags []string, rule config.VersionRule) Selection {
	var sel Selection
	var filter *regexp.Regexp
	if rule.TagFilter != "" {
		var err error
		if filter, err = regexp.Compile(rule.TagFilter); err != nil {
			sel.Problems = append(sel.Problems, Problem{
				Code:    CodeInvalidTagFilter,
				Message: "tag filter is not a valid RE2 expression",
				Hint:    "Correct the tag_filter expression of the version rule.",
			})
			return sel
		}
	}
	var out []parsedTag
	seen := map[string]bool{}
	for _, tag := range tags {
		p, kind := parseTag(tag)
		if kind != kindOK || seen[tag] || filter != nil && !filter.MatchString(tag) {
			continue
		}
		seen[tag] = true
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].less(out[j]) })
	for _, p := range out {
		sel.Available = append(sel.Available, p.tag)
	}
	return sel
}

// candidates returns the distinct versions above cur in cur's variant, lowest first. If a version exists
// with and without the "v" prefix, the spelling of the current tag wins.
func candidates(cur parsedTag, tags []string, filter *regexp.Regexp) []parsedTag {
	var out []parsedTag
	for _, tag := range tags {
		p, kind := parseTag(tag)
		if kind != kindOK || p.variant != cur.variant || !cur.less(p) {
			continue
		}
		if filter != nil && !filter.MatchString(tag) {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].sameVersion(out[j]) {
			return out[i].prefix == cur.prefix && out[j].prefix != cur.prefix
		}
		return out[i].less(out[j])
	})
	dedup := out[:0]
	for _, p := range out {
		if len(dedup) > 0 && dedup[len(dedup)-1].sameVersion(p) {
			continue
		}
		dedup = append(dedup, p)
	}
	return dedup
}

// target applies the update cap and the major-step rule to the sorted candidates above cur. With stepwise
// jumps the next major is the lowest major above the current one that has a candidate, so no published
// major is skipped.
func target(cur parsedTag, higher []parsedTag, maxUpdate, steps string) string {
	var capped []parsedTag
	for _, p := range higher {
		switch {
		case maxUpdate == config.LevelPatch && (p.major != cur.major || p.minor != cur.minor),
			maxUpdate == config.LevelMinor && p.major != cur.major:
			continue
		}
		capped = append(capped, p)
	}
	if len(capped) == 0 {
		return ""
	}
	if steps == config.MajorStep {
		// Sorted ascending, so the first entry carries the lowest major: the current one if it still has a
		// higher version, otherwise the next one.
		best := ""
		for _, p := range capped {
			if p.major == capped[0].major {
				best = p.tag
			}
		}
		return best
	}
	return capped[len(capped)-1].tag
}
