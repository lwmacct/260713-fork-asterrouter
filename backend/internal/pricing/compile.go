package pricing

import (
	"container/list"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

const (
	maxExpressionBytes = 8 * 1024
	maxASTNodes        = 256
	maxASTDepth        = 32
	maxStaticTiers     = 16
	maxStaticLines     = 32
	defaultCacheSize   = 512
)

var factNames = map[string]struct{}{
	"total_input_tokens": {}, "uncached_input_tokens": {}, "cache_read_tokens": {},
	"cache_write_5m_tokens": {}, "cache_write_1h_tokens": {}, "output_tokens": {}, "cache_fields_present": {},
	"input_images": {}, "output_images": {}, "partial_images": {}, "input_video_milliseconds": {},
	"output_video_milliseconds": {}, "input_audio_milliseconds": {}, "output_audio_milliseconds": {},
	"realtime_audio_milliseconds": {}, "input_characters": {}, "actions": {}, "batch_items": {},
	"input_bytes": {}, "output_bytes": {}, "transfer_bytes": {}, "session_milliseconds": {},
	"protocol": {}, "operation": {}, "modality": {}, "lane": {}, "stream": {}, "output_count": {}, "service_tier": {},
}

var functionNames = map[string]struct{}{
	"token_cost": {}, "unit_cost": {}, "block_cost": {}, "multiply_bps": {},
	"token_line": {}, "adjusted_token_line": {}, "unit_line": {}, "adjusted_unit_line": {},
	"block_line": {}, "adjusted_block_line": {}, "fixed_line": {}, "tier": {}, "min": {}, "max": {},
}

type cacheEntry struct {
	key  string
	rule CompiledRule
}

type compileCall struct {
	done chan struct{}
	rule CompiledRule
	err  error
}

type Engine struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	lru      *list.List
	inflight map[string]*compileCall
}

func NewEngine(capacity ...int) *Engine {
	size := defaultCacheSize
	if len(capacity) > 0 && capacity[0] > 0 {
		size = capacity[0]
	}
	return &Engine{capacity: size, entries: make(map[string]*list.Element), lru: list.New(), inflight: make(map[string]*compileCall)}
}

func (e *Engine) Compile(source string) (CompiledRule, error) {
	return e.CompileByHash(source, ExpressionHash(source))
}

func (e *Engine) CompileByHash(source, expectedHash string) (CompiledRule, error) {
	actualHash := ExpressionHash(source)
	if actualHash != strings.ToLower(strings.TrimSpace(expectedHash)) {
		return CompiledRule{}, errorf(ErrorHashMismatch, "expression hash does not match source")
	}
	key := strconv.Itoa(EngineVersionV1) + ":" + actualHash
	e.mu.Lock()
	if element, found := e.entries[key]; found {
		e.lru.MoveToFront(element)
		rule := element.Value.(cacheEntry).rule
		e.mu.Unlock()
		return rule, nil
	}
	if call, found := e.inflight[key]; found {
		e.mu.Unlock()
		<-call.done
		return call.rule, call.err
	}
	call := &compileCall{done: make(chan struct{})}
	e.inflight[key] = call
	e.mu.Unlock()

	rule, err := compileV1(source, actualHash)

	e.mu.Lock()
	call.rule, call.err = rule, err
	delete(e.inflight, key)
	if err == nil {
		element := e.lru.PushFront(cacheEntry{key: key, rule: rule})
		e.entries[key] = element
		for e.lru.Len() > e.capacity {
			last := e.lru.Back()
			delete(e.entries, last.Value.(cacheEntry).key)
			e.lru.Remove(last)
		}
	}
	close(call.done)
	e.mu.Unlock()
	return rule, err
}

func parseVersion(source string) (int, string, error) {
	if strings.TrimSpace(source) == "" {
		return 0, "", errorf(ErrorExpressionEmpty, "expression is required")
	}
	if len([]byte(source)) > maxExpressionBytes {
		return 0, "", errorf(ErrorExpressionTooLarge, "expression exceeds %d bytes", maxExpressionBytes)
	}
	if !strings.HasPrefix(source, "v1:") {
		return 0, "", errorf(ErrorVersionUnsupported, "expression must start with v1:")
	}
	body := strings.TrimSpace(strings.TrimPrefix(source, "v1:"))
	if body == "" {
		return 0, "", errorf(ErrorExpressionEmpty, "expression body is required")
	}
	return EngineVersionV1, body, nil
}

func compileV1(source, hash string) (CompiledRule, error) {
	version, body, err := parseVersion(source)
	if err != nil {
		return CompiledRule{}, err
	}
	tree, err := parser.Parse(body)
	if err != nil {
		return CompiledRule{}, errorf(ErrorCompileFailed, "%v", err)
	}
	analyzer := newASTAnalyzer()
	if err := analyzer.visit(tree.Node, 1, false); err != nil {
		return CompiledRule{}, err
	}
	if analyzer.nodes > maxASTNodes || analyzer.maxDepth > maxASTDepth {
		return CompiledRule{}, errorf(ErrorForbiddenAST, "expression exceeds AST limits")
	}
	if analyzer.tierCalls > maxStaticTiers || analyzer.lineCalls > maxStaticLines {
		return CompiledRule{}, errorf(ErrorForbiddenAST, "expression exceeds tier or line limits")
	}
	program, err := expr.Compile(body, expr.Env(compileEnvironment()), expr.DisableAllBuiltins(), expr.Patch(int64LiteralPatcher{}), expr.AsInt64())
	if err != nil {
		return CompiledRule{}, errorf(ErrorCompileFailed, "%v", err)
	}
	analysis := analyzer.analysis(version)
	return CompiledRule{Source: source, ExpressionHash: hash, EngineVersion: version, Analysis: analysis, body: body, program: program}, nil
}

type int64LiteralPatcher struct{}

func (int64LiteralPatcher) Visit(node *ast.Node) {
	if literal, ok := (*node).(*ast.IntegerNode); ok {
		ast.Patch(node, &ast.ConstantNode{Value: int64(literal.Value)})
	}
}

type astAnalyzer struct {
	nodes          int
	maxDepth       int
	tierCalls      int
	lineCalls      int
	requiredFacts  map[string]struct{}
	tiers          map[string]struct{}
	lineCodes      map[string]struct{}
	visualEditable bool
}

func newASTAnalyzer() *astAnalyzer {
	return &astAnalyzer{requiredFacts: map[string]struct{}{}, tiers: map[string]struct{}{}, lineCodes: map[string]struct{}{}, visualEditable: true}
}

func (a *astAnalyzer) visit(node ast.Node, depth int, callCallee bool) error {
	if node == nil {
		return errorf(ErrorForbiddenAST, "nil AST node")
	}
	a.nodes++
	if depth > a.maxDepth {
		a.maxDepth = depth
	}
	if a.nodes > maxASTNodes || depth > maxASTDepth {
		return errorf(ErrorForbiddenAST, "expression exceeds AST limits")
	}
	switch value := node.(type) {
	case *ast.IntegerNode, *ast.BoolNode:
		return nil
	case *ast.StringNode:
		if !validShortString(value.Value) {
			return errorf(ErrorForbiddenAST, "string literal is invalid or too long")
		}
		return nil
	case *ast.IdentifierNode:
		if callCallee {
			if _, allowed := functionNames[value.Value]; !allowed {
				return errorf(ErrorForbiddenAST, "function %q is not allowed", value.Value)
			}
			return nil
		}
		if _, allowed := factNames[value.Value]; !allowed {
			return errorf(ErrorForbiddenAST, "identifier %q is not allowed", value.Value)
		}
		a.requiredFacts[value.Value] = struct{}{}
		return nil
	case *ast.UnaryNode:
		if value.Operator != "!" {
			return errorf(ErrorForbiddenAST, "unary operator %q is not allowed", value.Operator)
		}
		return a.visit(value.Node, depth+1, false)
	case *ast.BinaryNode:
		if !allowedBinaryOperator(value.Operator) {
			return errorf(ErrorForbiddenAST, "binary operator %q is not allowed", value.Operator)
		}
		if err := a.visit(value.Left, depth+1, false); err != nil {
			return err
		}
		return a.visit(value.Right, depth+1, false)
	case *ast.ConditionalNode:
		if err := a.visit(value.Cond, depth+1, false); err != nil {
			return err
		}
		if err := a.visit(value.Exp1, depth+1, false); err != nil {
			return err
		}
		return a.visit(value.Exp2, depth+1, false)
	case *ast.CallNode:
		callee, ok := value.Callee.(*ast.IdentifierNode)
		if !ok {
			return errorf(ErrorForbiddenAST, "dynamic function calls are not allowed")
		}
		if err := a.visit(callee, depth+1, true); err != nil {
			return err
		}
		a.recordCall(callee.Value, value.Arguments)
		for _, argument := range value.Arguments {
			if err := a.visit(argument, depth+1, false); err != nil {
				return err
			}
		}
		return nil
	default:
		return errorf(ErrorForbiddenAST, "AST node %T is not allowed", node)
	}
}

func (a *astAnalyzer) recordCall(name string, arguments []ast.Node) {
	if name == "tier" {
		a.tierCalls++
		if len(arguments) > 0 {
			if literal, ok := arguments[0].(*ast.StringNode); ok && literal.Value != "" {
				a.tiers[literal.Value] = struct{}{}
			}
		}
	}
	if strings.HasSuffix(name, "_line") || name == "fixed_line" {
		a.lineCalls++
		if len(arguments) > 0 {
			if literal, ok := arguments[0].(*ast.StringNode); ok && literal.Value != "" {
				a.lineCodes[literal.Value] = struct{}{}
			}
		}
	}
	if name == "min" || name == "max" || strings.HasSuffix(name, "_cost") || name == "multiply_bps" {
		a.visualEditable = false
	}
}

func (a *astAnalyzer) analysis(version int) RuleAnalysis {
	required := sortedKeys(a.requiredFacts)
	tierNames := sortedKeys(a.tiers)
	tiers := make([]TierAnalysis, 0, len(tierNames))
	for _, name := range tierNames {
		tiers = append(tiers, TierAnalysis{Name: name})
	}
	return RuleAnalysis{EngineVersion: version, RequiredFacts: required, Tiers: tiers, LineCodes: sortedKeys(a.lineCodes), VisualEditable: a.visualEditable}
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func allowedBinaryOperator(operator string) bool {
	switch operator {
	case "+", "==", "!=", "<", "<=", ">", ">=", "&&", "||":
		return true
	default:
		return false
	}
}

func compileEnvironment() map[string]any {
	env := factsEnvironment(Facts{})
	env["token_cost"] = func(any, any) (int64, error) { return 0, nil }
	env["unit_cost"] = func(any, any) (int64, error) { return 0, nil }
	env["block_cost"] = func(any, any, any) (int64, error) { return 0, nil }
	env["multiply_bps"] = func(any, any) (int64, error) { return 0, nil }
	env["token_line"] = func(string, any, any) (int64, error) { return 0, nil }
	env["adjusted_token_line"] = func(string, any, any, any) (int64, error) { return 0, nil }
	env["unit_line"] = func(string, any, string, any) (int64, error) { return 0, nil }
	env["adjusted_unit_line"] = func(string, any, string, any, any) (int64, error) { return 0, nil }
	env["block_line"] = func(string, any, string, any, any) (int64, error) { return 0, nil }
	env["adjusted_block_line"] = func(string, any, string, any, any, any) (int64, error) { return 0, nil }
	env["fixed_line"] = func(string, string, any) (int64, error) { return 0, nil }
	env["tier"] = func(string, any) (int64, error) { return 0, nil }
	env["min"] = func(any, any) (int64, error) { return 0, nil }
	env["max"] = func(any, any) (int64, error) { return 0, nil }
	return env
}

func compiledProgram(rule CompiledRule) (*vm.Program, error) {
	if rule.program == nil || rule.EngineVersion != EngineVersionV1 || rule.ExpressionHash != ExpressionHash(rule.Source) {
		return nil, errorf(ErrorHashMismatch, "compiled rule is invalid")
	}
	return rule.program, nil
}

var _ = fmt.Sprintf
