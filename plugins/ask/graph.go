package ask

import (
	"regexp"
	"strings"
)

// CommandGraph is a lightweight graph over command ids. Edges connect commands
// in the same group (all job.*) and the same CRUD resource (create/update/
// delete on one resource), so graph expansion can pull in the full CRUD family
// that BM25 may have ranked lower. Mirrors TS graph.ts.
type CommandGraph struct {
	nodes    map[string]bool
	edges    map[string]map[string]bool
	resource map[string]map[string]bool
	group    map[string]map[string]bool
}

var crudResourceRe = regexp.MustCompile(`^([a-z]+)\.(create|update|delete|get|list|run|stop|log)\.?([a-z]*)$`)

// buildGraphFromCorpus builds the command graph from the command DocItems.
func buildGraphFromCorpus(corpus []DocItem) *CommandGraph {
	g := &CommandGraph{
		nodes:    map[string]bool{},
		edges:    map[string]map[string]bool{},
		resource: map[string]map[string]bool{},
		group:    map[string]map[string]bool{},
	}
	for _, item := range corpus {
		if item.Type != "command" {
			continue
		}
		id := item.ID
		g.nodes[id] = true

		if grp := strings.SplitN(id, ".", 2)[0]; grp != "" {
			addToSet(g.group, grp, id)
		}
		if m := crudResourceRe.FindStringSubmatch(id); m != nil {
			res := m[1]
			if m[3] != "" {
				res = m[1] + "." + m[3]
			}
			addToSet(g.resource, res, id)
		}
	}
	for _, members := range g.group {
		linkAll(g, members)
	}
	for _, members := range g.resource {
		linkAll(g, members)
	}
	return g
}

func addToSet(m map[string]map[string]bool, key, val string) {
	if m[key] == nil {
		m[key] = map[string]bool{}
	}
	m[key][val] = true
}

func linkAll(g *CommandGraph, members map[string]bool) {
	for a := range members {
		for b := range members {
			if a != b {
				addToSet(g.edges, a, b)
			}
		}
	}
}

// expandGraph returns command DocItems related to hits (same group / CRUD
// family) that aren't already in hits, up to maxExtra. Deterministic order:
// hits in order, neighbors in the order they appear in corpus.
func expandGraph(hits []DocItem, corpus []DocItem, g *CommandGraph, maxExtra int) []DocItem {
	if len(g.edges) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.ID] = true
	}
	idMap := map[string]DocItem{}
	var order []string // corpus order for deterministic neighbor emission
	for _, item := range corpus {
		if item.Type == "command" {
			if _, ok := idMap[item.ID]; !ok {
				order = append(order, item.ID)
			}
			idMap[item.ID] = item
		}
	}

	var extra []DocItem
	for _, hit := range hits {
		neigh := g.edges[hit.ID]
		if neigh == nil {
			continue
		}
		// Emit neighbors in corpus order for determinism (TS relies on Set
		// insertion order; corpus order is the stable analogue in Go).
		for _, nid := range order {
			if !neigh[nid] || seen[nid] {
				continue
			}
			seen[nid] = true
			if n, ok := idMap[nid]; ok {
				extra = append(extra, n)
				if len(extra) >= maxExtra {
					return extra
				}
			}
		}
	}
	return extra
}
