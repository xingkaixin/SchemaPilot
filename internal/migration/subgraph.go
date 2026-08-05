package migration

import "fmt"

// Subgraph restricts the graph to the target nodes plus every transitive
// dependency they need, preserving the original node order. An empty target
// list returns the graph unchanged.
func (graph Graph) Subgraph(targets []string) (Graph, error) {
	if len(targets) == 0 {
		return graph, nil
	}

	nodes := make(map[string]Node, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Name] = node
	}

	included := make(map[string]struct{})
	var include func(name string) error
	include = func(name string) error {
		if _, done := included[name]; done {
			return nil
		}
		node, exists := nodes[name]
		if !exists {
			return fmt.Errorf("unknown migration node %q", name)
		}
		included[name] = struct{}{}
		for _, dependency := range node.DependsOn {
			if err := include(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	for _, target := range targets {
		if err := include(target); err != nil {
			return Graph{}, err
		}
	}

	scoped := graph
	scoped.Nodes = make([]Node, 0, len(included))
	for _, node := range graph.Nodes {
		if _, exists := included[node.Name]; exists {
			scoped.Nodes = append(scoped.Nodes, node)
		}
	}
	return scoped, nil
}
