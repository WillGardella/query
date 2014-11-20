//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package algebra

import (
	"sort"

	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/value"
)

type ArrayAggDistinct struct {
	DistinctAggregateBase
}

func NewArrayAggDistinct(operand expression.Expression) Aggregate {
	return &ArrayAggDistinct{
		*NewDistinctAggregateBase("array_agg", operand),
	}
}

func (this *ArrayAggDistinct) String() string {
	return this.toString(this)
}

func (this *ArrayAggDistinct) Accept(visitor expression.Visitor) (interface{}, error) {
	return visitor.VisitFunction(this)
}

func (this *ArrayAggDistinct) Type() value.Type { return value.ARRAY }

func (this *ArrayAggDistinct) Evaluate(item value.Value, context expression.Context) (result value.Value, e error) {
	return this.evaluate(this, item, context)
}

func (this *ArrayAggDistinct) Constructor() expression.FunctionConstructor {
	return func(operands ...expression.Expression) expression.Function {
		return NewArrayAggDistinct(operands[0])
	}
}

func (this *ArrayAggDistinct) Default() value.Value { return value.NULL_VALUE }

func (this *ArrayAggDistinct) CumulateInitial(item, cumulative value.Value, context Context) (value.Value, error) {
	item, e := this.Operand().Evaluate(item, context)
	if e != nil {
		return nil, e
	}

	if item.Type() <= value.MISSING {
		return cumulative, nil
	}

	return setAdd(item, cumulative)
}

func (this *ArrayAggDistinct) CumulateIntermediate(part, cumulative value.Value, context Context) (value.Value, error) {
	return cumulateSets(part, cumulative)
}

func (this *ArrayAggDistinct) ComputeFinal(cumulative value.Value, context Context) (c value.Value, e error) {
	if cumulative == value.NULL_VALUE {
		return cumulative, nil
	}

	av := cumulative.(value.AnnotatedValue)
	set := av.GetAttachment("set").(*value.Set)
	if set.Len() == 0 {
		return value.NULL_VALUE, nil
	}

	actuals := set.Actuals()
	c = value.NewValue(actuals)
	sorter := value.NewSorter(c)
	sort.Sort(sorter)
	return c, nil
}
