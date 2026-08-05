package migration

import "testing"

func testGraph() Graph {
	return Graph{
		Version:     CurrentGraphVersion,
		Name:        "shop",
		Parallelism: 2,
		OnError:     ErrorPolicyHalt,
		Nodes: []Node{
			{Name: "user", Database: "primary", Scripts: []Script{{Path: "user.sql"}}},
			{Name: "order", Database: "primary", Scripts: []Script{{Path: "order.sql"}}},
			{Name: "report", Database: "reporting", DependsOn: []string{"user", "order"}, Scripts: []Script{{Path: "report.sql"}}},
		},
	}
}

func TestSubgraphIncludesTransitiveDependencies(t *testing.T) {
	scoped, err := testGraph().Subgraph([]string{"report"})
	if err != nil {
		t.Fatalf("Subgraph: %v", err)
	}
	names := nodeNames(scoped)
	if len(names) != 3 {
		t.Fatalf("expected all ancestors, got %v", names)
	}
}

func TestSubgraphKeepsOnlyRequestedBranch(t *testing.T) {
	scoped, err := testGraph().Subgraph([]string{"order"})
	if err != nil {
		t.Fatalf("Subgraph: %v", err)
	}
	names := nodeNames(scoped)
	if len(names) != 1 || names[0] != "order" {
		t.Fatalf("expected only order, got %v", names)
	}
}

func TestSubgraphPreservesNodeOrder(t *testing.T) {
	scoped, err := testGraph().Subgraph([]string{"report", "user"})
	if err != nil {
		t.Fatalf("Subgraph: %v", err)
	}
	names := nodeNames(scoped)
	want := []string{"user", "order", "report"}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("expected order %v, got %v", want, names)
		}
	}
}

func TestSubgraphRejectsUnknownTarget(t *testing.T) {
	if _, err := testGraph().Subgraph([]string{"missing"}); err == nil {
		t.Fatal("expected error for unknown node")
	}
}

func TestSubgraphWithoutTargetsReturnsGraphUnchanged(t *testing.T) {
	graph := testGraph()
	scoped, err := graph.Subgraph(nil)
	if err != nil {
		t.Fatalf("Subgraph: %v", err)
	}
	if len(scoped.Nodes) != len(graph.Nodes) {
		t.Fatalf("expected unchanged graph, got %v", nodeNames(scoped))
	}
}

func nodeNames(graph Graph) []string {
	names := make([]string, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		names = append(names, node.Name)
	}
	return names
}
