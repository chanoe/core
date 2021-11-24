package constraint

import (
	"errors"
)

const (
	ConstraintOpSearchFlush     = "searchCB"
	ConstraintOpTimeSeriesFlush = "timeseriesCB"
	ConstraintOpTypeConvert     = "convert"
)

var (
	ErrEntityConfigInvalid = errors.New("invalid entity configurations")
)

type Operator struct {
	Callback  string
	Condition interface{}
}

func newOperator(cb string, cond interface{}) Operator {
	return Operator{
		Callback:  cb,
		Condition: cond,
	}
}

type Constraint struct {
	ID         string
	Type       string
	Operators  []Operator
	ChildNodes []*Constraint
	EnableFlag *BitBucket
}

func newConstraint() *Constraint {
	return &Constraint{EnableFlag: NewBitBucket(8)}
}

func (ct *Constraint) GenSearchIndex() []string {
	return genSearchIndex("", ct)
}

func genSearchIndex(prefix string, ct *Constraint) []string {
	var searchIndexes []string
	if !ct.EnableFlag.Enabled(EnabledFlagSelf) {
		return []string{}
	}

	if ct.EnableFlag.Enabled(EnabledFlagSearch) {
		searchIndexes = append(searchIndexes, prefix+ct.ID)
	}

	for _, childCt := range ct.ChildNodes {
		searchIndexes = append(searchIndexes, genSearchIndex(ct.ID+".", childCt)...)
	}
	return searchIndexes
}

func NewConstraintsFrom(cfg Config) *Constraint {
	// current latyer.
	if !cfg.Enabled {
		return nil
	}

	return parseConstraintFrom(cfg)
}

func parseConstraintFrom(cfg Config) *Constraint {
	// current latyer.
	if !cfg.Enabled {
		return nil
	}

	ct := newConstraint()

	ct.ID = cfg.ID
	ct.Type = cfg.Type
	ct.EnableFlag.Enable(EnabledFlagSelf)
	if cfg.EnabledSearch {
		ct.EnableFlag.Enable(EnabledFlagSearch)
	}
	if cfg.EnabledTimeSeries {
		ct.EnableFlag.Enable(EnabledFlagTimeSeries)
	}

	switch cfg.Type {
	case PropertyTypeArray:
		define := cfg.getArrayDefine()
		if childCt := parseConstraintFrom(define.ElemType); nil != childCt {
			ct.ChildNodes = append(ct.ChildNodes, childCt)
		}
	case PropertyTypeStruct:
		define := cfg.getStructDefine()
		for _, field := range define.Fields {
			if childCt := parseConstraintFrom(field); nil != childCt {
				ct.ChildNodes = append(ct.ChildNodes, childCt)
			}
		}
	default:
		// TODO: .
	}

	// parse define.
	ct.Operators = parseDefine(cfg.Define)

	return ct
}

func parseDefine(define map[string]interface{}) []Operator {
	var ops []Operator
	for key, val := range define {
		if keyContains(key) {
			ops = append(ops, newOperator(key, val))
		}
	}
	return ops
}

func keyContains(key string) bool {
	return true
}

func ExecData(val Node, ct *Constraint) (Node, error) {
	return val, nil
}