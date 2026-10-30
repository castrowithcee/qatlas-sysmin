package adopt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
)

// MaxStackContainers bounds how many running containers ListStacks considers.
const MaxStackContainers = 1024

// stacksFormat prints one JSON object per container. json escapes whatever a label holds, so a label value
// cannot break out of its field; the values are still checked afterwards.
const stacksFormat = `{"id":{{json .ID}},"project":{{json (.Label "` + labelProject + `")}},` +
	`"dir":{{json (.Label "` + labelWorkingDir + `")}},"files":{{json (.Label "` + labelConfig + `")}},` +
	`"oneoff":{{json (.Label "` + labelOneOff + `")}}}`

// Stack is a running Compose stack found through the labels of its containers. Every field has been checked.
type Stack struct {
	ComposeProject string
	Dir            string
	ComposeFiles   []string
	// Containers counts the running containers, without one-off ones.
	Containers int
}

type stackRow struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Dir     string `json:"dir"`
	Files   string `json:"files"`
	OneOff  string `json:"oneoff"`
}

// ListStacks groups the running containers that carry Compose labels by Compose project and directory, in
// the order they were first seen. Containers with unusable labels are left out. truncated reports that
// containers beyond MaxStackContainers, or beyond what the runner kept of the output, were not considered.
func ListStacks(ctx context.Context, r execute.Runner) (stacks []Stack, truncated bool, err error) {
	res, err := r.Run(ctx, execute.Command{Name: "docker", Args: []string{
		"ps", "--no-trunc", "--filter", "label=" + labelProject, "--format", stacksFormat}})
	if err != nil {
		return nil, false, fmt.Errorf("docker ps failed: %w", err)
	}
	truncated = res.Omitted > 0
	type key struct{ project, dir string }
	index := map[key]int{}
	seen := 0
	for _, line := range strings.Split(string(res.Output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if seen++; seen > MaxStackContainers {
			truncated = true
			break
		}
		var row stackRow
		if json.Unmarshal([]byte(line), &row) != nil || row.OneOff == "True" || !containerIDRe.MatchString(row.ID) ||
			!composeNameRe.MatchString(row.Project) {
			continue
		}
		dir, ok := cleanAbs(row.Dir)
		if !ok {
			continue
		}
		files, ok := cleanAbsList(row.Files)
		if !ok {
			continue
		}
		k := key{row.Project, dir}
		i, known := index[k]
		if !known {
			i = len(stacks)
			index[k] = i
			stacks = append(stacks, Stack{ComposeProject: row.Project, Dir: dir, ComposeFiles: files})
		}
		stacks[i].Containers++
	}
	return stacks, truncated, nil
}

// cleanAbsList checks every entry of a comma-separated list of absolute paths.
func cleanAbsList(list string) ([]string, bool) {
	var out []string
	for _, f := range splitList(list) {
		c, ok := cleanAbs(f)
		if !ok {
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}
