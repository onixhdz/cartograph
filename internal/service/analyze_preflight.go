package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/onixhdz/cartograph/internal/ingestion"
)

func BuildAnalyzePreflight(req AnalyzePreflightRequest) (*AnalyzePreflightResult, error) {
	if req.Remote {
		return nil, errors.New("remote analyze preflight is not supported")
	}
	target := req.Target
	if target == "" {
		target = "."
	}
	result, err := ingestion.DetectRepoCandidates(target, ingestion.RepoDetectionOptions{})
	if err != nil {
		return nil, fmt.Errorf("detect repo candidates: %w", err)
	}
	candidates := analyzeRepoCandidates(result.Candidates)
	selected, required, err := analyzePreflightSelection(req.Selection, result.Candidates)
	if err != nil {
		return nil, err
	}
	res := NewAnalyzePreflightResult(req, candidates, analyzeRepoCandidates(selected), required)
	res.Target = target
	return &res, nil
}

func analyzeRepoCandidates(candidates []ingestion.RepoCandidate) []AnalyzeRepoCandidate {
	out := make([]AnalyzeRepoCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, AnalyzeRepoCandidate{
			Name:           candidate.Name,
			Path:           candidate.Path,
			RelPath:        candidate.RelPath,
			Signals:        analyzeRepoSignals(candidate.Signals),
			Classification: string(candidate.Classification),
			Recommended:    candidate.Recommended,
			SourceFiles:    candidate.SourceFiles,
			Parent:         candidate.Parent,
		})
	}
	return out
}

func analyzeRepoSignals(signals []ingestion.RepoSignal) []string {
	out := make([]string, len(signals))
	for i, signal := range signals {
		out[i] = string(signal)
	}
	return out
}

func analyzePreflightSelection(selection AnalyzeRepoSelection, candidates []ingestion.RepoCandidate) ([]ingestion.RepoCandidate, bool, error) {
	switch selection.Mode {
	case "", AnalyzeRepoSelectionDefault:
		return nil, shouldRequireAnalyzeRepoSelection(candidates), nil
	case AnalyzeRepoSelectionNone:
		return nil, false, nil
	case AnalyzeRepoSelectionAuto:
		selected := recommendedAnalyzeRepos(candidates)
		return selected, false, nil
	case AnalyzeRepoSelectionManual:
		selected, err := selectAnalyzeReposBySelector(candidates, selection.Selectors)
		return selected, false, err
	default:
		return nil, false, fmt.Errorf("unsupported repo selection mode %q", selection.Mode)
	}
}

func shouldRequireAnalyzeRepoSelection(candidates []ingestion.RepoCandidate) bool {
	count := 0
	for _, candidate := range candidates {
		if candidate.Recommended && candidate.RelPath != "" {
			count++
		}
	}
	return count > 1
}

func recommendedAnalyzeRepos(candidates []ingestion.RepoCandidate) []ingestion.RepoCandidate {
	var selected []ingestion.RepoCandidate
	for _, candidate := range candidates {
		if candidate.Recommended && candidate.RelPath != "" {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func selectAnalyzeReposBySelector(candidates []ingestion.RepoCandidate, selectors []string) ([]ingestion.RepoCandidate, error) {
	var selected []ingestion.RepoCandidate
	seen := make(map[string]bool)
	for _, selector := range selectors {
		selector = filepath.ToSlash(strings.TrimSpace(selector))
		if selector == "" {
			continue
		}
		var matches []ingestion.RepoCandidate
		for _, candidate := range candidates {
			if candidate.Name == selector || candidate.RelPath == selector {
				matches = append(matches, candidate)
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("repo selector %q did not match any detected repo candidate", selector)
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
			return nil, fmt.Errorf("repo selector %q is ambiguous; use relative path: %s", selector, strings.Join(paths, ", "))
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("select at least one repo candidate")
	}
	return selected, nil
}
