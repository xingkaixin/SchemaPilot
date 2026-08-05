package project

import "github.com/schemapilot/schemapilot/internal/migration"

// Scoped restricts a loaded project to the target nodes plus their transitive
// dependencies and refreshes the graph fingerprints so run records describe
// exactly the subgraph that executes. An empty target list returns the
// project unchanged.
func Scoped(loaded migration.Project, targets []string) (migration.Project, error) {
	if len(targets) == 0 {
		return loaded, nil
	}
	graph, err := loaded.Graph.Subgraph(targets)
	if err != nil {
		return migration.Project{}, err
	}
	if len(graph.Nodes) == len(loaded.Graph.Nodes) {
		return loaded, nil
	}

	scoped := loaded
	scoped.Graph = graph
	if scoped.Fingerprint, err = fingerprint(graph); err != nil {
		return migration.Project{}, err
	}
	if scoped.StructureFingerprint, err = structureFingerprint(graph); err != nil {
		return migration.Project{}, err
	}
	return scoped, nil
}
