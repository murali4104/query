//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package plan

import (
	"encoding/json"
)

type IntersectAll struct {
	readonly
	first       Operator
	second      Operator
	distinct    bool
	cost        float64
	cardinality float64
}

func NewIntersectAll(first, second Operator, distinct bool, cost, cardinality float64) *IntersectAll {
	return &IntersectAll{
		first:       first,
		second:      second,
		distinct:    distinct,
		cost:        cost,
		cardinality: cardinality,
	}
}

func (this *IntersectAll) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitIntersectAll(this)
}

func (this *IntersectAll) New() Operator {
	return &IntersectAll{}
}

func (this *IntersectAll) First() Operator {
	return this.first
}

func (this *IntersectAll) Second() Operator {
	return this.second
}

func (this *IntersectAll) Distinct() bool {
	return this.distinct
}

func (this *IntersectAll) Cost() float64 {
	return this.cost
}

func (this *IntersectAll) Cardinality() float64 {
	return this.cardinality
}

func (this *IntersectAll) MarshalJSON() ([]byte, error) {
	return json.Marshal(this.MarshalBase(nil))
}

func (this *IntersectAll) MarshalBase(f func(map[string]interface{})) map[string]interface{} {
	r := map[string]interface{}{"#operator": "IntersectAll"}
	if this.distinct {
		r["distinct"] = this.distinct
	}
	if this.cost > 0.0 {
		r["cost"] = this.cost
	}
	if this.cardinality > 0.0 {
		r["cardinality"] = this.cardinality
	}
	if f != nil {
		f(r)
	} else {
		r["first"] = this.first
		r["second"] = this.second
	}
	return r
}

func (this *IntersectAll) UnmarshalJSON(body []byte) error {
	var _unmarshalled struct {
		_           string          `json:"#operator"`
		First       json.RawMessage `json:"first"`
		Second      json.RawMessage `json:"second"`
		Distinct    bool            `json:"distinct"`
		Cost        float64         `json:"cost"`
		Cardinality float64         `json:"cardinality"`
	}

	err := json.Unmarshal(body, &_unmarshalled)
	if err != nil {
		return err
	}

	for i, child := range []json.RawMessage{_unmarshalled.First, _unmarshalled.Second} {
		var op_type struct {
			Operator string `json:"#operator"`
		}

		err = json.Unmarshal(child, &op_type)
		if err != nil {
			return err
		}

		if i == 0 {
			this.first, err = MakeOperator(op_type.Operator, child)
		} else {
			this.second, err = MakeOperator(op_type.Operator, child)
		}

		if err != nil {
			return err
		}
	}

	if _unmarshalled.Distinct {
		this.distinct = true
	}

	this.cost = getCost(_unmarshalled.Cost)
	this.cardinality = getCardinality(_unmarshalled.Cardinality)

	return err
}

func (this *IntersectAll) verify(prepared *Prepared) bool {
	return this.first.verify(prepared) && this.second.verify(prepared)
}
