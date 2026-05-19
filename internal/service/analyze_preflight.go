package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/realxen/cartograph/internal/ingestion"
)

func BuildAnalyzePreflight(req AnalyzePreflightRequest) (*AnalyzePreflightResult, error) {
	if req.Remote {
		return nil, errors.New("remote analyze preflight is not supported")
	}
	target := req.Target
	if target == "" {
		target = "."
	}
	result, err := ingestion.DetectProjects(target, ingestion.ProjectDetectionOptions{})
	if err != nil {
		return nil, fmt.Errorf("detect projects: %w", err)
	}
	candidates := analyzeProjectCandidates(result.Candidates)
	selected, required, err := analyzePreflightSelection(req.Selection, result.Candidates)
	if err != nil {
		return nil, err
	}
	res := NewAnalyzePreflightResult(req, candidates, analyzeProjectCandidates(selected), required)
	res.Target = target
	return &res, nil
}

func analyzeProjectCandidates(candidates []ingestion.ProjectCandidate) []AnalyzeProjectCandidate {
	out := make([]AnalyzeProjectCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, AnalyzeProjectCandidate{
			Name:           candidate.Name,
			Path:           candidate.Path,
			RelPath:        candidate.RelPath,
			Signals:        analyzeProjectSignals(candidate.Signals),
			Classification: string(candidate.Classification),
			Recommended:    candidate.Recommended,
			SourceFiles:    candidate.SourceFiles,
			Parent:         candidate.Parent,
		})
	}
	return out
}

func analyzeProjectSignals(signals []ingestion.ProjectSignal) []string {
	out := make([]string, len(signals))
	for i, signal := range signals {
		out[i] = string(signal)
	}
	return out
}

func analyzePreflightSelection(selection AnalyzeProjectSelection, candidates []ingestion.ProjectCandidate) ([]ingestion.ProjectCandidate, bool, error) {
	switch selection.Mode {
	case "", AnalyzeProjectSelectionDefault:
		return nil, shouldRequireAnalyzeProjectSelection(candidates), nil
	case AnalyzeProjectSelectionNone:
		return nil, false, nil
	case AnalyzeProjectSelectionAuto:
		selected := recommendedAnalyzeProjects(candidates)
		if len(selected) == 0 {
			return nil, false, errors.New("no recommended projects detected")
		}
		return selected, false, nil
	case AnalyzeProjectSelectionManual:
		selected, err := selectAnalyzeProjectsBySelector(candidates, selection.Selectors)
		return selected, false, err
	default:
		return nil, false, fmt.Errorf("unsupported project selection mode %q", selection.Mode)
	}
}

func shouldRequireAnalyzeProjectSelection(candidates []ingestion.ProjectCandidate) bool {
	count := 0
	for _, candidate := range candidates {
		if candidate.Recommended && candidate.RelPath != "" {
			count++
		}
	}
	return count > 1
}

func recommendedAnalyzeProjects(candidates []ingestion.ProjectCandidate) []ingestion.ProjectCandidate {
	var selected []ingestion.ProjectCandidate
	for _, candidate := range candidates {
		if candidate.Recommended && candidate.RelPath != "" {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func selectAnalyzeProjectsBySelector(candidates []ingestion.ProjectCandidate, selectors []string) ([]ingestion.ProjectCandidate, error) {
	var selected []ingestion.ProjectCandidate
	seen := make(map[string]bool)
	for _, selector := range selectors {
		selector = filepath.ToSlash(strings.TrimSpace(selector))
		if selector == "" {
			continue
		}
		var matches []ingestion.ProjectCandidate
		for _, candidate := range candidates {
			if candidate.Name == selector || candidate.RelPath == selector {
				matches = append(matches, candidate)
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("project selector %q did not match any detected project", selector)
		case 1:
			candidate := matches[0]
			if !seen[candidate.RelPath] {
				seen[candidate.RelPath] = true
				selected = append(selected, candidate)
			}
		default:
			paths := make([]string, len(matches))
			for i, match := range matches {
				paths[i] = match.RelPath
			}
			return nil, fmt.Errorf("project selector %q is ambiguous; use relative path: %s", selector, strings.Join(paths, ", "))
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("select at least one project")
	}
	return selected, nil
}
