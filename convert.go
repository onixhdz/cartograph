package cartograph

import (
	"maps"

	"github.com/onixhdz/cartograph/internal/service"
)

func convertQueryResult(in *service.QueryResult) *QueryResult {
	if in == nil {
		return nil
	}
	return &QueryResult{
		Processes:      convertProcessMatches(in.Processes),
		ProcessSymbols: convertSymbolMatches(in.ProcessSymbols),
		Definitions:    convertSymbolMatches(in.Definitions),
		UsageExamples:  convertSymbolMatches(in.UsageExamples),
		TestFlows:      convertProcessMatches(in.TestFlows),
		PluginResults:  convertPluginQueryMatches(in.PluginResults),
	}
}

func convertSearchResult(in *service.SearchResult) *SearchResult {
	if in == nil {
		return nil
	}
	return &SearchResult{
		Repo:         in.Repo,
		Pattern:      in.Pattern,
		FixedStrings: in.FixedStrings,
		IndexStatus:  in.IndexStatus,
		Message:      in.Message,
		DurationMS:   in.DurationMS,
		MatchCount:   in.MatchCount,
		FileCount:    in.FileCount,
		Truncated:    in.Truncated,
		Matches:      convertSearchMatches(in.Matches),
	}
}

func convertContextResult(in *service.ContextResult) *ContextResult {
	if in == nil {
		return nil
	}
	return &ContextResult{
		Symbol:             convertSymbolMatch(in.Symbol),
		Callers:            convertSymbolMatches(in.Callers),
		Callees:            convertSymbolMatches(in.Callees),
		CallTree:           convertCallTreeNode(in.CallTree),
		Importers:          convertSymbolMatches(in.Importers),
		Imports:            convertSymbolMatches(in.Imports),
		Processes:          convertSymbolMatches(in.Processes),
		Implementors:       convertSymbolMatches(in.Implementors),
		Extends:            convertSymbolMatches(in.Extends),
		RelationshipGroups: convertRelationshipGroups(in.RelationshipGroups),
		RelationshipStats:  convertRelationshipStats(in.RelationshipStats),
	}
}

func convertImpactResult(in *service.ImpactResult) *ImpactResult {
	if in == nil {
		return nil
	}
	return &ImpactResult{
		Target:   convertSymbolMatch(in.Target),
		Affected: convertSymbolMatches(in.Affected),
		Depth:    in.Depth,
	}
}

func convertCypherResult(in *service.CypherResult) *CypherResult {
	if in == nil {
		return nil
	}
	rows := make([]map[string]any, len(in.Rows))
	for i, row := range in.Rows {
		rows[i] = make(map[string]any, len(row))
		maps.Copy(rows[i], row)
	}
	return &CypherResult{Columns: append([]string(nil), in.Columns...), Rows: rows}
}

func convertSchemaResult(in *service.SchemaResult) *SchemaResult {
	if in == nil {
		return nil
	}
	return &SchemaResult{
		NodeLabels:           convertNodeLabels(in.NodeLabels),
		RelTypes:             convertRelTypes(in.RelTypes),
		RelationshipPatterns: convertRelationshipPatterns(in.RelationshipPatterns),
		Properties:           append([]string(nil), in.Properties...),
		TotalNodes:           in.TotalNodes,
		TotalEdges:           in.TotalEdges,
	}
}

func convertCatResult(in *service.CatResult) *CatResult {
	if in == nil {
		return nil
	}
	out := &CatResult{Files: make([]CatFile, len(in.Files))}
	for i, f := range in.Files {
		out.Files[i] = CatFile{Path: f.Path, Content: f.Content, LineCount: f.LineCount, Error: f.Error}
	}
	return out
}

func convertTreeResult(in *service.TreeResult) *TreeResult {
	if in == nil {
		return nil
	}
	return &TreeResult{Repo: in.Repo, Files: append([]string(nil), in.Files...)}
}

func convertListResult(in *service.ListResult) *ListResult {
	if in == nil {
		return nil
	}
	return &ListResult{Repos: convertRepoInfos(in.Repos)}
}

func convertStatusResult(in *service.StatusResult) *StatusResult {
	if in == nil {
		return nil
	}
	return &StatusResult{
		Name:              in.Name,
		Hash:              in.Hash,
		Path:              in.Path,
		URL:               in.URL,
		Type:              in.Type,
		Indexed:           in.Indexed,
		IndexedAt:         in.IndexedAt,
		NodeCount:         in.NodeCount,
		EdgeCount:         in.EdgeCount,
		Commit:            in.Commit,
		Branch:            in.Branch,
		Languages:         append([]string(nil), in.Languages...),
		Duration:          in.Duration,
		BuiltWith:         in.BuiltWith,
		EmbeddingStatus:   in.EmbeddingStatus,
		EmbeddingProgress: in.EmbeddingProgress,
		EmbeddingTotal:    in.EmbeddingTotal,
		EmbeddingModel:    in.EmbeddingModel,
		EmbeddingProvider: in.EmbeddingProvider,
		EmbeddingDims:     in.EmbeddingDims,
		EmbeddingError:    in.EmbeddingError,
		Artifacts:         convertRepoArtifacts(in.Artifacts),
	}
}

func convertProcessMatches(in []service.ProcessMatch) []ProcessMatch {
	out := make([]ProcessMatch, len(in))
	for i, m := range in {
		out[i] = ProcessMatch{
			Name:           m.Name,
			HeuristicLabel: m.HeuristicLabel,
			StepCount:      m.StepCount,
			CallerCount:    m.CallerCount,
			Importance:     m.Importance,
			Relevance:      m.Relevance,
		}
	}
	return out
}

func convertSymbolMatches(in []service.SymbolMatch) []SymbolMatch {
	out := make([]SymbolMatch, len(in))
	for i, m := range in {
		out[i] = convertSymbolMatch(m)
	}
	return out
}

func convertSymbolMatch(in service.SymbolMatch) SymbolMatch {
	return SymbolMatch{
		Name:        in.Name,
		FilePath:    in.FilePath,
		StartLine:   in.StartLine,
		EndLine:     in.EndLine,
		Label:       in.Label,
		ProcessName: in.ProcessName,
		Content:     in.Content,
		Score:       in.Score,
		Repo:        in.Repo,
		Signature:   in.Signature,
	}
}

func convertSearchMatches(in []service.SearchMatch) []SearchMatch {
	out := make([]SearchMatch, len(in))
	for i, m := range in {
		out[i] = SearchMatch{
			FilePath: m.FilePath,
			Line:     m.Line,
			Column:   m.Column,
			LineText: m.LineText,
			Before:   append([]string(nil), m.Before...),
			After:    append([]string(nil), m.After...),
		}
		if m.Symbol != nil {
			sym := convertSymbolMatch(*m.Symbol)
			out[i].Symbol = &sym
		}
	}
	return out
}

func convertPluginQueryMatches(in []service.PluginQueryMatch) []PluginQueryMatch {
	out := make([]PluginQueryMatch, len(in))
	for i, m := range in {
		out[i] = PluginQueryMatch{
			EntityLabel: m.EntityLabel,
			NodeID:      m.NodeID,
			Score:       m.Score,
			Fields:      convertPluginDisplayFields(m.Fields),
		}
	}
	return out
}

func convertPluginDisplayFields(in []service.PluginDisplayField) []PluginDisplayField {
	out := make([]PluginDisplayField, len(in))
	for i, f := range in {
		out[i] = PluginDisplayField{Label: f.Label, Value: f.Value}
	}
	return out
}

func convertCallTreeNode(in *service.CallTreeNode) *CallTreeNode {
	if in == nil {
		return nil
	}
	return &CallTreeNode{
		Symbol:   convertSymbolMatch(in.Symbol),
		EdgeType: in.EdgeType,
		Children: convertCallTreeNodes(in.Children),
		Pruned:   in.Pruned,
	}
}

func convertCallTreeNodes(in []service.CallTreeNode) []CallTreeNode {
	out := make([]CallTreeNode, len(in))
	for i := range in {
		out[i] = *convertCallTreeNode(&in[i])
	}
	return out
}

func convertRelationshipGroups(in []service.RelationshipGroup) []RelationshipGroup {
	out := make([]RelationshipGroup, len(in))
	for i, g := range in {
		out[i] = RelationshipGroup{Type: g.Type, Relationships: convertContextRelationships(g.Relationships)}
	}
	return out
}

func convertContextRelationships(in []service.ContextRelationship) []ContextRelationship {
	out := make([]ContextRelationship, len(in))
	for i, r := range in {
		out[i] = ContextRelationship{
			FromID: r.FromID,
			From:   convertSymbolMatch(r.From),
			ToID:   r.ToID,
			To:     convertSymbolMatch(r.To),
		}
	}
	return out
}

func convertRelationshipStats(in *service.RelationshipStats) *RelationshipStats {
	if in == nil {
		return nil
	}
	return &RelationshipStats{
		Depth:                 in.Depth,
		ReturnedNodes:         in.ReturnedNodes,
		ReturnedRelationships: in.ReturnedRelationships,
		Limit:                 in.Limit,
		Truncated:             in.Truncated,
	}
}

func convertRepoInfos(in []service.RepoInfo) []RepoInfo {
	out := make([]RepoInfo, len(in))
	for i, r := range in {
		out[i] = RepoInfo{
			Name:      r.Name,
			Hash:      r.Hash,
			Type:      r.Type,
			IndexedAt: r.IndexedAt,
			NodeCount: r.NodeCount,
			EdgeCount: r.EdgeCount,
			BuiltWith: r.BuiltWith,
			Embedding: r.Embedding,
		}
	}
	return out
}

func convertRepoArtifacts(in []service.RepoArtifact) []RepoArtifact {
	out := make([]RepoArtifact, len(in))
	for i, a := range in {
		out[i] = RepoArtifact{Name: a.Name, Bytes: a.Bytes}
	}
	return out
}

func convertNodeLabels(in []service.NodeLabelSummary) []NodeLabelSummary {
	out := make([]NodeLabelSummary, len(in))
	for i, n := range in {
		out[i] = NodeLabelSummary{Label: n.Label, Count: n.Count}
	}
	return out
}

func convertRelTypes(in []service.RelTypeSummary) []RelTypeSummary {
	out := make([]RelTypeSummary, len(in))
	for i, r := range in {
		out[i] = RelTypeSummary{Type: r.Type, Count: r.Count}
	}
	return out
}

func convertRelationshipPatterns(in []service.RelationshipPatternSummary) []RelationshipPatternSummary {
	out := make([]RelationshipPatternSummary, len(in))
	for i, p := range in {
		out[i] = RelationshipPatternSummary{From: p.From, Type: p.Type, To: p.To, Count: p.Count}
	}
	return out
}
