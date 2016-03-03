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

	"github.com/couchbase/query/algebra"
	"github.com/couchbase/query/datastore"
	"github.com/couchbase/query/value"
)

// Create index
type InferKeyspace struct {
	readonly
	keyspace datastore.Keyspace
	node     *algebra.InferKeyspace
}

func NewInferKeyspace(keyspace datastore.Keyspace, node *algebra.InferKeyspace) *InferKeyspace {
	return &InferKeyspace{
		keyspace: keyspace,
		node:     node,
	}
}

func (this *InferKeyspace) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitInferKeyspace(this)
}

func (this *InferKeyspace) New() Operator {
	return &InferKeyspace{}
}

func (this *InferKeyspace) Keyspace() datastore.Keyspace {
	return this.keyspace
}

func (this *InferKeyspace) Node() *algebra.InferKeyspace {
	return this.node
}

func (this *InferKeyspace) MarshalJSON() ([]byte, error) {
	r := map[string]interface{}{"#operator": "InferKeyspace"}
	r["keyspace"] = this.keyspace.Name()
	r["namespace"] = this.keyspace.NamespaceId()
	r["using"] = this.node.Using()

	if this.node.With() != nil {
		r["with"] = this.node.With()
	}
	if this.duration != 0 {
		r["#time"] = this.duration.String()
	}

	return json.Marshal(r)
}

func (this *InferKeyspace) UnmarshalJSON(body []byte) error {
	var _unmarshalled struct {
		_      string                  `json:"#operator"`
		Keysp  string                  `json:"keyspace"`
		Namesp string                  `json:"namespace"`
		Using  datastore.InferenceType `json:"using"`
		With   json.RawMessage         `json:"with"`
	}

	err := json.Unmarshal(body, &_unmarshalled)
	if err != nil {
		return err
	}

	this.keyspace, err = datastore.GetKeyspace(_unmarshalled.Namesp, _unmarshalled.Keysp)
	if err != nil {
		return err
	}

	ksref := algebra.NewKeyspaceRef(_unmarshalled.Namesp, _unmarshalled.Keysp, "")

	var with value.Value
	if len(_unmarshalled.With) > 0 {
		with = value.NewValue(_unmarshalled.With)
	}

	this.node = algebra.NewInferKeyspace(ksref, _unmarshalled.Using, with)
	return nil
}
