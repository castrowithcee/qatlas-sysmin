package adopt

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// stackLine is one line of the output of docker ps with the format ListStacks asks for.
func stackLine(id, project, dir, files, oneoff string) string {
	b, _ := json.Marshal(map[string]string{"id": id, "project": project, "dir": dir, "files": files, "oneoff": oneoff})
	return string(b) + "\n"
}

func TestListStacksGroupsAndSkipsOneOff(t *testing.T) {
	f := &fakeDocker{stacks: stackLine(idA, "demo", "/srv/demo-stack", "/srv/demo-stack/compose.yml,/srv/demo-stack/./extra.yml", "False") +
		stackLine(idB, "demo", "/srv/demo-stack/", "/srv/demo-stack/compose.yml,/srv/demo-stack/extra.yml", "False") +
		stackLine("0123456789ac", "demo", "/srv/demo-stack", "", "True") +
		stackLine("0123456789ad", "other", "/srv/other", "/srv/other/compose.yml", "False")}
	stacks, truncated, err := ListStacks(context.Background(), f)
	if err != nil || truncated {
		t.Fatalf("err = %v, truncated = %v", err, truncated)
	}
	if len(stacks) != 2 || stacks[0].ComposeProject != "demo" || stacks[0].Dir != "/srv/demo-stack" || stacks[0].Containers != 2 ||
		!slices.Equal(stacks[0].ComposeFiles, []string{"/srv/demo-stack/compose.yml", "/srv/demo-stack/extra.yml"}) ||
		stacks[1].ComposeProject != "other" || stacks[1].Containers != 1 {
		t.Fatalf("stacks = %+v", stacks)
	}
	call := f.calls[0]
	if call.Name != "docker" || call.Args[0] != "ps" || slices.Contains(call.Args, "-a") || slices.Contains(call.Args, "--all") ||
		!slices.Contains(call.Args, "label="+labelProject) {
		t.Errorf("call = %+v", call)
	}
}

func TestListStacksDropsInvalidValues(t *testing.T) {
	f := &fakeDocker{stacks: stackLine("not-an-id", "demo", "/srv/a", "", "False") +
		stackLine(idA, "Bad Name", "/srv/b", "", "False") +
		stackLine(idA, "demo", "relative/dir", "", "False") +
		stackLine(idA, "demo", "/srv/c\nx", "", "False") +
		stackLine(idA, "demo", "/srv/d", "/srv/d/ok.yml,relative.yml", "False") +
		"garbage\n{broken\n" +
		stackLine(idB, "demo", "/srv/demo-stack", "", "False")}
	stacks, _, err := ListStacks(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(stacks) != 1 || stacks[0].Dir != "/srv/demo-stack" || len(stacks[0].ComposeFiles) != 0 {
		t.Fatalf("stacks = %+v", stacks)
	}
}

func TestListStacksTruncates(t *testing.T) {
	var b strings.Builder
	for i := range MaxStackContainers + 1 {
		b.WriteString(stackLine(fmt.Sprintf("%012x", i), "demo", "/srv/demo-stack", "", "False"))
	}
	stacks, truncated, err := ListStacks(context.Background(), &fakeDocker{stacks: b.String()})
	if err != nil || !truncated || len(stacks) != 1 || stacks[0].Containers != MaxStackContainers {
		t.Fatalf("stacks = %+v, truncated = %v, err = %v", stacks, truncated, err)
	}
	stacks, truncated, err = ListStacks(context.Background(), &fakeDocker{stacks: stackLine(idA, "demo", "/srv/demo-stack", "", "False")})
	if err != nil || truncated || len(stacks) != 1 {
		t.Fatalf("small list: %+v, %v, %v", stacks, truncated, err)
	}
}
