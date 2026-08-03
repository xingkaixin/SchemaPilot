package migration

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const CurrentGraphVersion = 1

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

type ValidationError struct {
	Problems []string
}

func (validationError *ValidationError) Error() string {
	return "invalid migration graph: " + strings.Join(validationError.Problems, "; ")
}

func ValidateProject(project Project) error {
	problems := validateGraph(project.Graph, project.Databases)
	if len(problems) == 0 {
		return nil
	}

	sort.Strings(problems)
	return &ValidationError{Problems: problems}
}

func TopologicalOrder(graph Graph) ([]string, error) {
	indegree := make(map[string]int, len(graph.Nodes))
	dependents := make(map[string][]string, len(graph.Nodes))
	for _, node := range graph.Nodes {
		indegree[node.Name] = len(node.DependsOn)
		for _, dependency := range node.DependsOn {
			dependents[dependency] = append(dependents[dependency], node.Name)
		}
	}

	ready := make([]string, 0, len(graph.Nodes))
	for name, degree := range indegree {
		if degree == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(graph.Nodes))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)

		children := append([]string(nil), dependents[name]...)
		sort.Strings(children)
		for _, child := range children {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
				sort.Strings(ready)
			}
		}
	}

	if len(order) != len(graph.Nodes) {
		return nil, errors.New("migration graph contains a dependency cycle")
	}

	return order, nil
}

func validateGraph(graph Graph, databases map[string]DatabaseProfile) []string {
	problems := make([]string, 0)
	if graph.Version != CurrentGraphVersion {
		problems = append(problems, fmt.Sprintf("version must be %d", CurrentGraphVersion))
	}
	if !namePattern.MatchString(graph.Name) {
		problems = append(problems, "name must start with a letter and contain only letters, digits, underscores, or hyphens")
	}
	if graph.Parallelism < 1 {
		problems = append(problems, "parallelism must be at least 1")
	}
	if !graph.OnError.IsValid() {
		problems = append(problems, "on_error must be halt or continue")
	}
	if len(graph.Nodes) == 0 {
		problems = append(problems, "at least one node is required")
	}

	nodeNames := make(map[string]struct{}, len(graph.Nodes))
	scriptOwners := make(map[string]string)
	for _, node := range graph.Nodes {
		if _, exists := nodeNames[node.Name]; exists {
			problems = append(problems, fmt.Sprintf("node %q is defined more than once", node.Name))
		}
		nodeNames[node.Name] = struct{}{}
	}

	for _, node := range graph.Nodes {
		problems = append(problems, validateNode(node, nodeNames, databases, scriptOwners)...)
	}

	if _, err := TopologicalOrder(graph); err != nil {
		problems = append(problems, err.Error())
	}

	for name, profile := range databases {
		if name != profile.Name {
			problems = append(problems, fmt.Sprintf("database profile map key %q does not match profile name %q", name, profile.Name))
		}
		if !profile.Driver.IsValid() {
			problems = append(problems, fmt.Sprintf("database profile %q has unsupported driver %q", name, profile.Driver))
		}
		if strings.TrimSpace(profile.DSN) == "" {
			problems = append(problems, fmt.Sprintf("database profile %q has an empty DSN", name))
		}
	}

	return problems
}

func validateNode(node Node, nodeNames map[string]struct{}, databases map[string]DatabaseProfile, scriptOwners map[string]string) []string {
	problems := make([]string, 0)
	if !namePattern.MatchString(node.Name) {
		problems = append(problems, fmt.Sprintf("node %q has an invalid name", node.Name))
	}
	if _, exists := databases[node.Database]; !exists {
		problems = append(problems, fmt.Sprintf("node %q references unknown database profile %q", node.Name, node.Database))
	}
	if node.ErrorPolicy != "" && !node.ErrorPolicy.IsValid() {
		problems = append(problems, fmt.Sprintf("node %q on_error must be halt or continue", node.Name))
	}
	if len(node.Scripts) == 0 {
		problems = append(problems, fmt.Sprintf("node %q must contain at least one script", node.Name))
	}

	dependencies := make(map[string]struct{}, len(node.DependsOn))
	for _, dependency := range node.DependsOn {
		if dependency == node.Name {
			problems = append(problems, fmt.Sprintf("node %q cannot depend on itself", node.Name))
		}
		if _, exists := nodeNames[dependency]; !exists {
			problems = append(problems, fmt.Sprintf("node %q depends on unknown node %q", node.Name, dependency))
		}
		if _, exists := dependencies[dependency]; exists {
			problems = append(problems, fmt.Sprintf("node %q lists dependency %q more than once", node.Name, dependency))
		}
		dependencies[dependency] = struct{}{}
	}

	for _, script := range node.Scripts {
		if strings.TrimSpace(script.Path) == "" {
			problems = append(problems, fmt.Sprintf("node %q contains a script with an empty path", node.Name))
			continue
		}
		if strings.TrimSpace(script.Checksum) == "" {
			problems = append(problems, fmt.Sprintf("script %q has no checksum", script.Path))
		}
		if owner, exists := scriptOwners[script.Path]; exists {
			problems = append(problems, fmt.Sprintf("script %q is used by both node %q and node %q", script.Path, owner, node.Name))
		}
		scriptOwners[script.Path] = node.Name
	}

	return problems
}
