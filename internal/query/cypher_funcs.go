// Package query registers standard Cypher functions that are missing from
// the cloudprivacylabs/opencypher library and implements post-execution
// aggregation for count(), collect(), sum(), avg(), min(), max().
package query

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/cloudprivacylabs/opencypher"

	"github.com/realxen/cartograph/internal/graph"
)

func init() {
	registerScalarFuncs()
	registerAggregationStubs()
}

// Scalar functions — fully correct per-row.

func registerScalarFuncs() {
	opencypher.RegisterGlobalFunc(
		// id(node) — returns the node's graph ID property.
		opencypher.Function{
			Name: "id", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				switch v := args[0].Get().(type) {
				case *lpg.Node:
					return opencypher.RValue{Value: graph.GetStringProp(v, graph.PropID)}, nil
				case *lpg.Edge:
					return opencypher.RValue{Value: fmt.Sprintf("%p", v)}, nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// keys(node/map) — returns property keys.
		opencypher.Function{
			Name: "keys", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				switch v := args[0].Get().(type) {
				case *lpg.Node:
					var keys []opencypher.Value
					v.ForEachProperty(func(key string, _ any) bool {
						keys = append(keys, opencypher.RValue{Value: key})
						return true
					})
					return opencypher.RValue{Value: keys}, nil
				}
				return opencypher.RValue{Value: []opencypher.Value{}}, nil
			},
		},

		// properties(node) — returns a map of all properties.
		opencypher.Function{
			Name: "properties", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				switch v := args[0].Get().(type) {
				case *lpg.Node:
					m := make(map[string]any)
					v.ForEachProperty(func(key string, value any) bool {
						m[key] = value
						return true
					})
					return opencypher.RValue{Value: m}, nil
				}
				return opencypher.RValue{Value: map[string]any{}}, nil
			},
		},

		// exists(expr) — returns true if the value is not null.
		opencypher.Function{
			Name: "exists", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				return opencypher.RValue{Value: args[0].Get() != nil}, nil
			},
		},

		// toInteger(expr)
		opencypher.Function{
			Name: "toInteger", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				switch v := args[0].Get().(type) {
				case int:
					return opencypher.RValue{Value: v}, nil
				case float64:
					return opencypher.RValue{Value: int(v)}, nil
				case string:
					var i int
					if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
						return opencypher.RValue{Value: i}, nil
					}
				}
				return opencypher.RValue{}, nil
			},
		},

		// toString(expr)
		opencypher.Function{
			Name: "toString", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				return opencypher.RValue{Value: fmt.Sprintf("%v", args[0].Get())}, nil
			},
		},

		// toLower(string)
		opencypher.Function{
			Name: "toLower", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if s, ok := args[0].Get().(string); ok {
					return opencypher.RValue{Value: strings.ToLower(s)}, nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// toUpper(string)
		opencypher.Function{
			Name: "toUpper", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if s, ok := args[0].Get().(string); ok {
					return opencypher.RValue{Value: strings.ToUpper(s)}, nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// trim(string)
		opencypher.Function{
			Name: "trim", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if s, ok := args[0].Get().(string); ok {
					return opencypher.RValue{Value: strings.TrimSpace(s)}, nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// head(list) — first element.
		opencypher.Function{
			Name: "head", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if arr, ok := args[0].Get().([]opencypher.Value); ok && len(arr) > 0 {
					return arr[0], nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// last(list) — last element.
		opencypher.Function{
			Name: "last", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if arr, ok := args[0].Get().([]opencypher.Value); ok && len(arr) > 0 {
					return arr[len(arr)-1], nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// tail(list) — all but first.
		opencypher.Function{
			Name: "tail", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if arr, ok := args[0].Get().([]opencypher.Value); ok && len(arr) > 1 {
					return opencypher.RValue{Value: arr[1:]}, nil
				}
				return opencypher.RValue{Value: []opencypher.Value{}}, nil
			},
		},

		// length(path/list/string)
		opencypher.Function{
			Name: "length", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				switch v := args[0].Get().(type) {
				case []opencypher.Value:
					return opencypher.RValue{Value: len(v)}, nil
				case []*lpg.Edge:
					return opencypher.RValue{Value: len(v)}, nil
				case string:
					return opencypher.RValue{Value: len(v)}, nil
				}
				return opencypher.RValue{Value: 0}, nil
			},
		},

		// coalesce(expr, expr, ...) — first non-null.
		opencypher.Function{
			Name: "coalesce", MinArgs: 1, MaxArgs: -1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				for _, a := range args {
					if a.Get() != nil {
						return a, nil
					}
				}
				return opencypher.RValue{}, nil
			},
		},

		// nodes(path) — returns nodes in a path.
		opencypher.Function{
			Name: "nodes", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if p, ok := args[0].Get().(*lpg.Path); ok {
					var nodes []opencypher.Value
					for i := 0; i <= p.NumEdges(); i++ {
						nodes = append(nodes, opencypher.RValue{Value: p.GetNode(i)})
					}
					return opencypher.RValue{Value: nodes}, nil
				}
				return opencypher.RValue{Value: []opencypher.Value{}}, nil
			},
		},

		// relationships(path) — returns edges in a path.
		opencypher.Function{
			Name: "relationships", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if p, ok := args[0].Get().(*lpg.Path); ok {
					var edges []opencypher.Value
					for i := range p.NumEdges() {
						edges = append(edges, opencypher.RValue{Value: p.GetEdge(i)})
					}
					return opencypher.RValue{Value: edges}, nil
				}
				return opencypher.RValue{Value: []opencypher.Value{}}, nil
			},
		},

		// startNode(relationship)
		opencypher.Function{
			Name: "startNode", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if e, ok := args[0].Get().(*lpg.Edge); ok {
					return opencypher.RValue{Value: e.GetFrom()}, nil
				}
				return opencypher.RValue{}, nil
			},
		},

		// endNode(relationship)
		opencypher.Function{
			Name: "endNode", MinArgs: 1, MaxArgs: 1,
			ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
				if e, ok := args[0].Get().(*lpg.Edge); ok {
					return opencypher.RValue{Value: e.GetTo()}, nil
				}
				return opencypher.RValue{}, nil
			},
		},
	)
}

// Aggregation stubs — return per-row markers, post-processed by aggregateCypherResult.

func registerAggregationStubs() {
	// count(x) — returns 1 for non-null, 0 for null (summed in post-processing).
	opencypher.RegisterGlobalFunc(opencypher.Function{
		Name: "count", MinArgs: 1, MaxArgs: 1,
		ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
			if args[0].Get() == nil {
				return opencypher.RValue{Value: 0}, nil
			}
			return opencypher.RValue{Value: 1}, nil
		},
	})

	// sum(x) — returns value as-is (summed in post-processing).
	opencypher.RegisterGlobalFunc(opencypher.Function{
		Name: "sum", MinArgs: 1, MaxArgs: 1,
		ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
			return args[0], nil
		},
	})

	// avg(x) — returns value as-is (averaged in post-processing).
	opencypher.RegisterGlobalFunc(opencypher.Function{
		Name: "avg", MinArgs: 1, MaxArgs: 1,
		ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
			return args[0], nil
		},
	})

	// min(x) — returns value as-is (min'd in post-processing).
	opencypher.RegisterGlobalFunc(opencypher.Function{
		Name: "min", MinArgs: 1, MaxArgs: 1,
		ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
			return args[0], nil
		},
	})

	// max(x) — returns value as-is (max'd in post-processing).
	opencypher.RegisterGlobalFunc(opencypher.Function{
		Name: "max", MinArgs: 1, MaxArgs: 1,
		ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
			return args[0], nil
		},
	})

	// collect(x) — returns value as-is (collected into list in post-processing).
	opencypher.RegisterGlobalFunc(opencypher.Function{
		Name: "collect", MinArgs: 1, MaxArgs: 1,
		ValueFunc: func(_ *opencypher.EvalContext, args []opencypher.Value) (opencypher.Value, error) {
			return args[0], nil
		},
	})
}

// Post-execution aggregation.

// aggFuncPattern matches aggregation function calls in RETURN clauses.
var aggFuncPattern = regexp.MustCompile(`(?i)\b(count|sum|avg|min|max|collect)\s*\(`)

// aggregateCypherResult post-processes the ResultSet to collapse rows for
// aggregation functions (count/sum/avg/min/max/collect).
// Supports pure aggregation and group-by aggregation.
func aggregateCypherResult(query string, rs *opencypher.ResultSet) *opencypher.ResultSet {
	if rs == nil || len(rs.Rows) <= 1 {
		return rs
	}

	// Find which RETURN columns use aggregation by parsing the query.
	aggCols := detectAggColumns(query, rs.Cols)
	if len(aggCols) == 0 {
		return rs // no aggregation detected
	}

	// Determine group-by columns (any non-aggregation column).
	var groupCols []string
	for _, col := range rs.Cols {
		if _, isAgg := aggCols[col]; !isAgg {
			groupCols = append(groupCols, col)
		}
	}

	if len(groupCols) == 0 {
		// Pure aggregation: collapse all rows into one.
		result := opencypher.ResultSet{Cols: rs.Cols}
		aggRow := make(map[string]opencypher.Value)
		for col, op := range aggCols {
			aggRow[col] = applyAggregation(op, col, rs.Rows)
		}
		result.Rows = []map[string]opencypher.Value{aggRow}
		return &result
	}

	// Group-by aggregation: group rows by non-agg columns, aggregate within each group.
	type group struct {
		key  string // serialized group-by values
		rows []map[string]opencypher.Value
		repr map[string]opencypher.Value // first row's non-agg values
	}
	groups := make(map[string]*group)
	var order []string

	for _, row := range rs.Rows {
		var keyParts []string
		for _, gc := range groupCols {
			keyParts = append(keyParts, fmt.Sprintf("%v", row[gc].Get()))
		}
		key := strings.Join(keyParts, "\x00")
		if g, ok := groups[key]; ok {
			g.rows = append(g.rows, row)
		} else {
			repr := make(map[string]opencypher.Value)
			for _, gc := range groupCols {
				repr[gc] = row[gc]
			}
			groups[key] = &group{key: key, rows: []map[string]opencypher.Value{row}, repr: repr}
			order = append(order, key)
		}
	}

	result := opencypher.ResultSet{Cols: rs.Cols}
	for _, key := range order {
		g := groups[key]
		outRow := make(map[string]opencypher.Value)
		maps.Copy(outRow, g.repr)
		for col, op := range aggCols {
			outRow[col] = applyAggregation(op, col, g.rows)
		}
		result.Rows = append(result.Rows, outRow)
	}
	return &result
}

// detectAggColumns parses the RETURN clause to find which columns use
// aggregation functions. Returns a map of column name → aggregation op.
func detectAggColumns(query string, cols []string) map[string]string {
	result := make(map[string]string)

	// Extract the RETURN clause.
	upper := strings.ToUpper(query)
	retIdx := strings.LastIndex(upper, "RETURN")
	if retIdx < 0 {
		return result
	}
	returnClause := query[retIdx+6:]

	// Remove ORDER BY, LIMIT, SKIP suffixes.
	for _, kw := range []string{"ORDER BY", "LIMIT", "SKIP"} {
		if idx := strings.Index(strings.ToUpper(returnClause), kw); idx >= 0 {
			returnClause = returnClause[:idx]
		}
	}

	// Split by comma (respecting parentheses nesting).
	exprs := splitReturnExprs(returnClause)

	for i, expr := range exprs {
		expr = strings.TrimSpace(expr)
		lowerExpr := strings.ToLower(expr)

		// Check for aggregation function call.
		for _, fn := range []string{"count", "sum", "avg", "min", "max", "collect"} {
			if strings.Contains(lowerExpr, fn+"(") {
				// Determine the column name: use AS alias if present, else positional.
				colName := extractAlias(expr)
				if colName == "" && i < len(cols) {
					colName = cols[i]
				}
				if colName != "" {
					result[colName] = fn
				}
				break
			}
		}
	}
	return result
}

// splitReturnExprs splits the RETURN clause by commas, respecting parentheses.
func splitReturnExprs(clause string) []string {
	var exprs []string
	depth := 0
	start := 0
	for i, ch := range clause {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				exprs = append(exprs, clause[start:i])
				start = i + 1
			}
		}
	}
	exprs = append(exprs, clause[start:])
	return exprs
}

// extractAlias returns the AS alias from an expression like "count(n) AS cnt".
func extractAlias(expr string) string {
	upper := strings.ToUpper(expr)
	idx := strings.LastIndex(upper, " AS ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(expr[idx+4:])
}

// applyAggregation aggregates values in a column across rows.
func applyAggregation(op, col string, rows []map[string]opencypher.Value) opencypher.Value {
	switch op {
	case "count":
		total := 0
		for _, row := range rows {
			if v, ok := row[col]; ok {
				if n, ok := v.Get().(int); ok {
					total += n
				} else if v.Get() != nil {
					total++
				}
			}
		}
		return opencypher.RValue{Value: total}

	case "sum":
		sum := 0.0
		for _, row := range rows {
			if v, ok := row[col]; ok {
				sum += toFloat(v)
			}
		}
		return opencypher.RValue{Value: sum}

	case "avg":
		sum := 0.0
		count := 0
		for _, row := range rows {
			if v, ok := row[col]; ok && v.Get() != nil {
				sum += toFloat(v)
				count++
			}
		}
		if count == 0 {
			return opencypher.RValue{}
		}
		return opencypher.RValue{Value: sum / float64(count)}

	case "min":
		minVal := math.MaxFloat64
		found := false
		for _, row := range rows {
			if v, ok := row[col]; ok && v.Get() != nil {
				f := toFloat(v)
				if f < minVal {
					minVal = f
					found = true
				}
			}
		}
		if !found {
			return opencypher.RValue{}
		}
		return opencypher.RValue{Value: minVal}

	case "max":
		maxVal := -math.MaxFloat64
		found := false
		for _, row := range rows {
			if v, ok := row[col]; ok && v.Get() != nil {
				f := toFloat(v)
				if f > maxVal {
					maxVal = f
					found = true
				}
			}
		}
		if !found {
			return opencypher.RValue{}
		}
		return opencypher.RValue{Value: maxVal}

	case "collect":
		var list []opencypher.Value
		for _, row := range rows {
			if v, ok := row[col]; ok && v.Get() != nil {
				list = append(list, v)
			}
		}
		return opencypher.RValue{Value: list}
	}

	return opencypher.RValue{}
}

func toFloat(v opencypher.Value) float64 {
	switch n := v.Get().(type) {
	case int:
		return float64(n)
	case float64:
		return n
	case int64:
		return float64(n)
	}
	return 0
}

// hasAggregation returns true if the query contains aggregation function calls.
func hasAggregation(query string) bool {
	return aggFuncPattern.MatchString(query)
}
