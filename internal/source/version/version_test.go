package version

import (
	"reflect"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

func TestSelectTarget(t *testing.T) {
	major := []string{"1.2.3", "1.2.4", "1.3.0", "2.0.0", "2.1.0"}
	tests := []struct {
		name    string
		current string
		tags    []string
		rule    config.VersionRule
		want    string
	}{
		{"default rule takes the highest", "1.2.3", major, config.VersionRule{}, "2.1.0"},
		{"patch stays in major.minor", "1.2.3", major, config.VersionRule{MaxUpdate: "patch"}, "1.2.4"},
		{"minor stays in major", "1.2.3", major, config.VersionRule{MaxUpdate: "minor"}, "1.3.0"},
		{"major takes everything", "1.2.3", major, config.VersionRule{MaxUpdate: "major"}, "2.1.0"},
		{"patch without patch release has no target", "1.3.0", major, config.VersionRule{MaxUpdate: "patch"}, ""},
		{"never lower than current", "2.1.0", major, config.VersionRule{}, ""},
		{"stepwise takes highest of current major first", "27.0.3",
			[]string{"27.0.3", "27.1.0", "27.2.5", "28.0.1", "28.4.0", "29.0.0"},
			config.VersionRule{MajorSteps: "stepwise"}, "27.2.5"},
		{"stepwise moves to the next major from there", "27.2.5",
			[]string{"27.2.5", "28.0.1", "28.4.0", "29.0.0"},
			config.VersionRule{MajorSteps: "stepwise"}, "28.4.0"},
		{"stepwise never jumps two majors", "28.4.0",
			[]string{"28.4.0", "29.0.0", "29.1.0", "30.0.0"},
			config.VersionRule{MajorSteps: "stepwise"}, "29.1.0"},
		{"stepwise skips an unpublished major", "27.0.0",
			[]string{"27.0.0", "29.0.0", "29.1.0"},
			config.VersionRule{MajorSteps: "stepwise"}, "29.1.0"},
		{"direct jumps to the highest", "27.0.3",
			[]string{"27.1.0", "28.4.0", "29.0.0"},
			config.VersionRule{MajorSteps: "direct"}, "29.0.0"},
		{"stepwise is capped by minor", "27.0.3",
			[]string{"27.1.0", "28.4.0"},
			config.VersionRule{MaxUpdate: "minor", MajorSteps: "stepwise"}, "27.1.0"},
		{"variant keeps its suffix", "29.0.1-apache",
			[]string{"29.0.1-apache", "29.0.2", "29.0.2-fpm", "29.0.2-apache", "29.0.1-fpm"},
			config.VersionRule{}, "29.0.2-apache"},
		{"v prefix is accepted on both sides", "v1.0.0", []string{"v1.0.0", "v1.1.0", "1.2.0"}, config.VersionRule{}, "1.2.0"},
		{"same version with and without v prefers the current spelling", "v1.0.0", []string{"1.1.0", "v1.1.0"}, config.VersionRule{}, "v1.1.0"},
		{"pre-releases are never chosen", "1.0.0",
			[]string{"1.1.0-rc1", "1.1.0-beta.2", "2.0.0-alpha", "1.0.1-1", "1.0.1-snapshot", "1.0.1-dev"},
			config.VersionRule{}, ""},
		{"incomplete and foreign tags are ignored", "1.0.0",
			[]string{"1.2", "16", "latest", "1.1.0.1", "v1", "stable", "01.2.3", "main", "1.0.1"},
			config.VersionRule{}, "1.0.1"},
		{"filter narrows the candidates", "1.0.0",
			[]string{"1.1.0", "1.2.0", "2.0.0"},
			config.VersionRule{TagFilter: `^1\.1\.`}, "1.1.0"},
		{"filter can empty the candidates", "1.0.0", []string{"1.1.0"}, config.VersionRule{TagFilter: `^9\.`}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Select(tc.current, tc.tags, tc.rule)
			if got.Target != tc.want {
				t.Errorf("target = %q, want %q (problems %v)", got.Target, tc.want, got.Problems)
			}
			if len(got.Problems) != 0 {
				t.Errorf("unexpected problems: %v", got.Problems)
			}
		})
	}
}

func TestSelectAvailableIsSortedAndIgnoresCap(t *testing.T) {
	tags := []string{"2.0.0", "1.10.0", "1.2.4", "1.2.3", "1.9.0", "1.2.4", "latest"}
	got := Select("1.2.3", tags, config.VersionRule{MaxUpdate: "patch"})
	want := []string{"1.2.4", "1.9.0", "1.10.0", "2.0.0"}
	if !reflect.DeepEqual(got.Available, want) {
		t.Errorf("available = %v, want %v", got.Available, want)
	}
	if got.Target != "1.2.4" || got.Current != "1.2.3" {
		t.Errorf("current/target = %q/%q", got.Current, got.Target)
	}
}

func TestSelectFloatingCurrentTag(t *testing.T) {
	for _, current := range []string{"latest", "16", "1.2", "16-alpine", "", "stable"} {
		got := Select(current, []string{"1.0.0", "2.0.0"}, config.VersionRule{})
		if len(got.Problems) != 1 || got.Problems[0].Code != CodeFloatingTag {
			t.Errorf("%q: problems = %v, want one %s", current, got.Problems, CodeFloatingTag)
			continue
		}
		if got.Problems[0].Message == "" || got.Problems[0].Hint == "" {
			t.Errorf("%q: problem lacks message or hint: %+v", current, got.Problems[0])
		}
		if got.Target != "" || len(got.Available) != 0 {
			t.Errorf("%q: unexpected selection %+v", current, got)
		}
	}
}

func TestSelectPrereleaseCurrentTag(t *testing.T) {
	got := Select("1.0.0-rc1", []string{"1.0.0"}, config.VersionRule{})
	if len(got.Problems) != 1 || got.Problems[0].Code != CodePrereleaseTag || got.Target != "" {
		t.Errorf("got %+v", got)
	}
}

func TestSelectInvalidRule(t *testing.T) {
	got := Select("1.0.0", []string{"1.1.0"}, config.VersionRule{TagFilter: "("})
	if len(got.Problems) != 1 || got.Problems[0].Code != CodeInvalidTagFilter || got.Target != "" {
		t.Errorf("tag filter: %+v", got)
	}
	got = Select("1.0.0", []string{"1.1.0"}, config.VersionRule{MaxUpdate: "huge"})
	if len(got.Problems) != 1 || got.Problems[0].Code != CodeInvalidRule || got.Target != "" || len(got.Available) != 1 {
		t.Errorf("rule: %+v", got)
	}
}

func TestParseTagKinds(t *testing.T) {
	tests := []struct {
		tag  string
		kind parseKind
	}{
		{"1.2.3", kindOK}, {"v1.2.3", kindOK}, {"1.2.3-alpine3.19", kindOK}, {"1.2.3-apache", kindOK},
		{"1.2.3-rc.1", kindPrerelease}, {"1.2.3-RC2", kindPrerelease}, {"1.2.3-0.3", kindPrerelease},
		{"1.2.3-beta-alpine", kindPrerelease}, {"1.2", kindInvalid}, {"V1.2.3", kindInvalid},
		{"99999999999999999999.0.0", kindInvalid},
	}
	for _, tc := range tests {
		if _, kind := parseTag(tc.tag); kind != tc.kind {
			t.Errorf("%q: kind %d, want %d", tc.tag, kind, tc.kind)
		}
	}
}
