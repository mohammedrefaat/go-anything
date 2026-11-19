// Package planner calculates dependency waves from package import graphs,
// detects and collapses strongly connected components (import cycles), and
// orders units into topological migration execution waves.
package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// PlanResult contains the planned units organized into topological migration waves.
type PlanResult struct {
	TotalUnits int                        `json:"total_units"`
	MaxWave    int                        `json:"max_wave"`
	Waves      map[int][]*adapterkit.Unit `json:"waves"`
	Units      []*adapterkit.Unit         `json:"units"`
	Cycles     [][]string                 `json:"cycles,omitempty"`
}

// Planner computes execution waves from dependency graphs.
type Planner struct{}

// New returns a new Planner instance.
func New() *Planner {
	return &Planner{}
}

// PlanFromAnalyzerResult plans migration waves from static analysis output.
func (p *Planner) PlanFromAnalyzerResult(res *analyzer.Result) (*PlanResult, error) {
	if res == nil {
		return nil, fmt.Errorf("analyzer result cannot be nil")
	}

	graph := make(map[string][]string, len(res.InternalGraph))
	for k, v := range res.InternalGraph {
		graph[k] = append([]string{}, v...)
	}

	// Ensure all packages exist in graph even if they have no dependencies
	for k := range res.Packages {
		if _, exists := graph[k]; !exists {
			graph[k] = []string{}
		}
	}

	plan := p.PlanWaves(graph)

	// Enrich units with package metadata
	for _, unit := range plan.Units {
		pkgPath, _ := unit.Metadata["pkg_path"].(string)
		if pkg, ok := res.Packages[pkgPath]; ok {
			unit.Metadata["files"] = pkg.Files
			unit.Metadata["public_apis"] = pkg.PublicAPIs
			unit.Metadata["types"] = pkg.Types
			unit.Metadata["dir"] = pkg.Dir
			unit.Metadata["package_name"] = pkg.Name
		}
	}

	return plan, nil
}

// PlanWaves computes topological waves from a directed dependency graph (node -> dependencies).
// Nodes with 0 internal dependencies are placed in Wave 0 (leaves).
// Cycles are detected via Tarjan's SCC algorithm and grouped into the same wave.
func (p *Planner) PlanWaves(graph map[string][]string) *PlanResult {
	// 1. Detect Strongly Connected Components (SCCs) via Tarjan's algorithm
	sccs := FindSCCs(graph)

	// Collect detected cycles (SCCs with size > 1)
	var cycles [][]string
	nodeToComp := make(map[string]int)
	compToNodes := make(map[int][]string)

	for compID, scc := range sccs {
		compToNodes[compID] = scc
		for _, node := range scc {
			nodeToComp[node] = compID
		}
		if len(scc) > 1 {
			cycles = append(cycles, scc)
		}
	}

	// 2. Build condensed Component DAG (compID -> slice of dependency compIDs)
	compDeps := make(map[int][]int)
	for compID, nodes := range compToNodes {
		seen := make(map[int]bool)
		for _, node := range nodes {
			for _, dep := range graph[node] {
				depComp, ok := nodeToComp[dep]
				if ok && depComp != compID && !seen[depComp] {
					seen[depComp] = true
					compDeps[compID] = append(compDeps[compID], depComp)
				}
			}
		}
		sort.Ints(compDeps[compID])
	}

	// 3. Compute waves for each component using memoized depth
	compWaves := make(map[int]int)
	visited := make(map[int]bool)

	var computeWave func(compID int) int
	computeWave = func(compID int) int {
		if w, computed := compWaves[compID]; computed {
			return w
		}
		if visited[compID] {
			// Safety check in condensed DAG
			return 0
		}
		visited[compID] = true

		deps := compDeps[compID]
		if len(deps) == 0 {
			compWaves[compID] = 0
			return 0
		}

		maxDepWave := 0
		for _, depComp := range deps {
			w := computeWave(depComp)
			if w > maxDepWave {
				maxDepWave = w
			}
		}

		resWave := maxDepWave + 1
		compWaves[compID] = resWave
		return resWave
	}

	for compID := range compToNodes {
		computeWave(compID)
	}

	// 4. Construct Unit objects for all packages
	maxWave := 0
	wavesMap := make(map[int][]*adapterkit.Unit)
	var allUnits []*adapterkit.Unit

	for compID, nodes := range compToNodes {
		waveNum := compWaves[compID]
		if waveNum > maxWave {
			maxWave = waveNum
		}

		isCycle := len(nodes) > 1

		for _, node := range nodes {
			// Dependencies of this unit (filtered to remove cycle peers)
			var filteredDeps []string
			peerMap := make(map[string]bool, len(nodes))
			for _, n := range nodes {
				peerMap[n] = true
			}

			for _, dep := range graph[node] {
				if !peerMap[dep] {
					filteredDeps = append(filteredDeps, toUnitID(dep))
				}
			}
			sort.Strings(filteredDeps)

			unitID := toUnitID(node)
			unitName := unitID
			if lastSlash := strings.LastIndex(unitID, "/"); lastSlash != -1 {
				unitName = unitID[lastSlash+1:]
			}

			meta := map[string]any{
				"pkg_path": node,
				"in_cycle": isCycle,
			}
			if isCycle {
				var peers []string
				for _, n := range nodes {
					if n != node {
						peers = append(peers, toUnitID(n))
					}
				}
				sort.Strings(peers)
				meta["cycle_peers"] = peers
			}

			unit := &adapterkit.Unit{
				SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
				ID:              unitID,
				Name:            unitName,
				Wave:            waveNum,
				Dependencies:    filteredDeps,
				State:           adapterkit.StatePlanned,
				EvidenceProfile: adapterkit.ProfileFull,
				Metadata:        meta,
			}

			wavesMap[waveNum] = append(wavesMap[waveNum], unit)
			allUnits = append(allUnits, unit)
		}
	}

	// 5. Deterministic sorting (Rule 2)
	for w := range wavesMap {
		sort.Slice(wavesMap[w], func(i, j int) bool {
			return wavesMap[w][i].ID < wavesMap[w][j].ID
		})
	}

	sort.Slice(allUnits, func(i, j int) bool {
		if allUnits[i].Wave != allUnits[j].Wave {
			return allUnits[i].Wave < allUnits[j].Wave
		}
		return allUnits[i].ID < allUnits[j].ID
	})

	return &PlanResult{
		TotalUnits: len(allUnits),
		MaxWave:    maxWave,
		Waves:      wavesMap,
		Units:      allUnits,
		Cycles:     cycles,
	}
}

func toUnitID(pkgPath string) string {
	parts := strings.Split(pkgPath, "/")
	var id string
	if len(parts) >= 2 {
		id = strings.Join(parts[len(parts)-2:], "/")
	} else {
		id = pkgPath
	}
	id = strings.TrimPrefix(id, "modern/")
	id = strings.TrimPrefix(id, "legacy/")
	return id
}

// FindSCCs implements Tarjan's strongly connected components algorithm.
// Returns deterministic slice of SCCs, each containing node names sorted ascending.
func FindSCCs(graph map[string][]string) [][]string {
	var allNodes []string
	for k := range graph {
		allNodes = append(allNodes, k)
	}
	sort.Strings(allNodes)

	index := 0
	stack := []string{}
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlink := make(map[string]int)

	for _, n := range allNodes {
		indices[n] = -1
		lowlink[n] = -1
	}

	var sccs [][]string

	var strongConnect func(v string)
	strongConnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		// Sort neighbors for deterministic execution (Rule 2)
		neighbors := append([]string{}, graph[v]...)
		sort.Strings(neighbors)

		for _, w := range neighbors {
			if indices[w] == -1 {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sort.Strings(scc)
			sccs = append(sccs, scc)
		}
	}

	for _, v := range allNodes {
		if indices[v] == -1 {
			strongConnect(v)
		}
	}

	// Sort SCCs deterministically by first node
	sort.Slice(sccs, func(i, j int) bool {
		return sccs[i][0] < sccs[j][0]
	})

	return sccs
}
