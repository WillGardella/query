//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package expression

import (
	"github.com/couchbaselabs/query/value"
)

type Within struct {
	binaryBase
}

func NewWithin(first, second Expression) Expression {
	return &Within{
		binaryBase{
			first:  first,
			second: second,
		},
	}
}

func (this *Within) Evaluate(item value.Value, context Context) (value.Value, error) {
	return this.evaluate(this, item, context)
}

func (this *Within) EquivalentTo(other Expression) bool {
	return this.equivalentTo(this, other)
}

func (this *Within) Fold() (Expression, error) {
	return this.fold(this)
}

func (this *Within) Formalize(allowed value.Value, keyspace string) (Expression, error) {
	return this.formalize(this, allowed, keyspace)
}

func (this *Within) SubsetOf(other Expression) bool {
	return this.subsetOf(this, other)
}

func (this *Within) VisitChildren(visitor Visitor) (Expression, error) {
	return this.visitChildren(this, visitor)
}

func (this *Within) eval(first, second value.Value) (value.Value, error) {
	if first.Type() == value.MISSING || second.Type() == value.MISSING {
		return value.MISSING_VALUE, nil
	}

	desc := make([]interface{}, 0, 64)
	desc = second.Descendants(desc)
	for _, d := range desc {
		if first.Equals(value.NewValue(d)) {
			return value.TRUE_VALUE, nil
		}
	}

	return value.FALSE_VALUE, nil
}

func NewNotWithin(first, second Expression) Expression {
	return NewNot(NewWithin(first, second))
}