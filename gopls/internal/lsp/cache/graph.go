// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"sort"

	"golang.org/x/tools/gopls/internal/lsp/source"
	"golang.org/x/tools/gopls/internal/span"
)

// A metadataGraph is an immutable and transitively closed import
// graph of Go packages, as obtained from go/packages.
type metadataGraph struct {
	// metadata maps package IDs to their associated metadata.
	metadata map[PackageID]*source.Metadata

	// importedBy maps package IDs to the list of packages that import them.
	importedBy map[PackageID][]PackageID

	// ids maps file URIs to package IDs, sorted by (!valid, cli, packageID).
	// A single file may belong to multiple packages due to tests packages.
	ids map[span.URI][]PackageID
}

// Metadata implements the source.MetadataSource interface.
func (g *metadataGraph) Metadata(id PackageID) *source.Metadata {
	return g.metadata[id]
}

// Clone creates a new metadataGraph, applying the given updates to the
// receiver.
func (g *metadataGraph) Clone(updates map[PackageID]*source.Metadata) *metadataGraph {
	if len(updates) == 0 {
		// Optimization: since the graph is immutable, we can return the receiver.
		return g
	}
	result := &metadataGraph{metadata: make(map[PackageID]*source.Metadata, len(g.metadata))}
	// Copy metadata.
	for id, m := range g.metadata {
		result.metadata[id] = m
	}
	for id, m := range updates {
		if m == nil {
			delete(result.metadata, id)
		} else {
			result.metadata[id] = m
		}
	}
	result.build()
	return result
}

// build constructs g.importedBy and g.uris from g.metadata.
//
// TODO(rfindley): we should enforce that the graph is acyclic here.
func (g *metadataGraph) build() {
	// Build the import graph.
	g.importedBy = make(map[PackageID][]PackageID)
	for id, m := range g.metadata {
		for _, depID := range m.DepsByPkgPath {
			g.importedBy[depID] = append(g.importedBy[depID], id)
		}
	}

	// Collect file associations.
	g.ids = make(map[span.URI][]PackageID)
	for id, m := range g.metadata {
		uris := map[span.URI]struct{}{}
		for _, uri := range m.CompiledGoFiles {
			uris[uri] = struct{}{}
		}
		for _, uri := range m.GoFiles {
			uris[uri] = struct{}{}
		}
		for uri := range uris {
			g.ids[uri] = append(g.ids[uri], id)
		}
	}

	// Sort and filter file associations.
	for uri, ids := range g.ids {
		sort.Slice(ids, func(i, j int) bool {
			cli := source.IsCommandLineArguments(ids[i])
			clj := source.IsCommandLineArguments(ids[j])
			if cli != clj {
				return clj
			}

			// 2. packages appear in name order.
			return ids[i] < ids[j]
		})

		// Choose the best IDs for each URI, according to the following rules:
		//  - If there are any valid real packages, choose them.
		//  - Else, choose the first valid command-line-argument package, if it exists.
		//
		// TODO(rfindley): it might be better to track all IDs here, and exclude
		// them later when type checking, but this is the existing behavior.
		for i, id := range ids {
			// If we've seen *anything* prior to command-line arguments package, take
			// it. Note that ids[0] may itself be command-line-arguments.
			if i > 0 && source.IsCommandLineArguments(id) {
				g.ids[uri] = ids[:i]
				break
			}
		}
	}
}

// reverseReflexiveTransitiveClosure returns a new mapping containing the
// metadata for the specified packages along with any package that
// transitively imports one of them, keyed by ID, including all the initial packages.
func (g *metadataGraph) reverseReflexiveTransitiveClosure(ids ...PackageID) map[PackageID]*source.Metadata {
	seen := make(map[PackageID]*source.Metadata)
	var visitAll func([]PackageID)
	visitAll = func(ids []PackageID) {
		for _, id := range ids {
			if seen[id] == nil {
				if m := g.metadata[id]; m != nil {
					seen[id] = m
					visitAll(g.importedBy[id])
				}
			}
		}
	}
	visitAll(ids)
	return seen
}
