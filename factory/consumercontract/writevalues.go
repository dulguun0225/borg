package consumercontract

import (
	"fmt"
	"go/ast"
	"go/token"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// Written values are facts about source assignments. Mirror tags describe the
// expected producer form and cannot establish what a consumer actually writes.
// This pass follows scalar literals only. Unknown values leave the derivation
// partial, with no invented population, domain, or range assertion.
func (s *consumerSource) literalWrites(element string) ([]any, bool) {
	var values []any
	for _, expression := range s.values[element] {
		value, ok := scalarLiteral(expression)
		if !ok {
			s.cannotFollow(fmt.Sprintf("a written value of %s that is not a scalar literal", element))
			return nil, false
		}
		values = append(values, value)
	}
	return values, len(values) > 0
}

func scalarLiteral(expression ast.Expr) (any, bool) {
	switch e := expression.(type) {
	case *ast.ParenExpr:
		return scalarLiteral(e.X)
	case *ast.Ident:
		switch e.Name {
		case "nil":
			return nil, true
		case "true":
			return true, true
		case "false":
			return false, true
		}
	case *ast.UnaryExpr:
		value, ok := scalarLiteral(e.X)
		if number, numeric := value.(float64); ok && numeric {
			if e.Op == token.SUB {
				return -number, true
			}
			if e.Op == token.ADD {
				return number, true
			}
		}
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			value, err := strconv.Unquote(e.Value)
			return value, err == nil
		}
		if e.Kind == token.INT || e.Kind == token.FLOAT {
			// ParseFloat does not accept Go integer bases; ParseInt does.
			text := strings.ReplaceAll(e.Value, "_", "")
			if e.Kind == token.INT {
				value, err := strconv.ParseInt(text, 0, 64)
				return float64(value), err == nil && value >= -(1<<53) && value <= 1<<53
			}
			value, err := strconv.ParseFloat(text, 64)
			return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
		}
	}
	return nil, false
}

func (s *consumerSource) sendsPopulated(add func(string, gatepolicy.PredicateKind, string) error, e contract.Element) error {
	values, known := s.literalWrites(e.Name)
	if !known {
		return nil
	}
	for _, value := range values {
		if value == nil {
			return nil
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			return nil
		}
	}
	return add(e.Name, gatepolicy.PredicatePopulated, "")
}

func (s *consumerSource) sendsInside(add func(string, gatepolicy.PredicateKind, string) error, e contract.Element) error {
	values, known := s.literalWrites(e.Name)
	if !known {
		return nil
	}
	var names []string
	low, high := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		switch value := value.(type) {
		case string:
			// The domain syntax cannot represent an empty value or a separator.
			if value == "" || strings.TrimSpace(value) != value || strings.Contains(value, "|") {
				s.cannotFollow(fmt.Sprintf("a written string of %s outside the domain argument syntax", e.Name))
				return nil
			}
			if !slices.Contains(names, value) {
				names = append(names, value)
			}
		case float64:
			low = math.Min(low, value)
			high = math.Max(high, value)
		default:
			return nil
		}
	}
	if len(names) > 0 && math.IsInf(low, 1) {
		slices.Sort(names)
		return add(e.Name, gatepolicy.PredicateSentDomain, contract.DomainText(names))
	}
	if len(names) == 0 && !math.IsInf(low, 1) {
		return add(e.Name, gatepolicy.PredicateSentRange, (contract.Range{Low: low, High: high}).Text())
	}
	s.cannotFollow(fmt.Sprintf("incompatible scalar writes to %s", e.Name))
	return nil
}
