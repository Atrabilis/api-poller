package extract

import (
	"fmt"
	"math"
	"reflect"
)

func MatchesAll(item any, conditions []Condition) (bool, error) {
	for _, condition := range conditions {
		value, err := Resolve(item, condition.Path)
		if err != nil {
			return false, err
		}
		if !equalValues(value, condition.Equals) {
			return false, nil
		}
	}
	return true, nil
}

type Condition struct {
	Path   string
	Equals any
}

func Numeric(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("value %v (%T) is not numeric", value, value)
	}
}

func equalValues(left, right any) bool {
	lf, lok := normalizeNumber(left)
	rf, rok := normalizeNumber(right)
	if lok && rok {
		return math.Abs(lf-rf) < 1e-9
	}
	return reflect.DeepEqual(left, right)
}

func normalizeNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}
