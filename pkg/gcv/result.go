// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcv

import (
	"encoding/json"

	"github.com/forseti-security/config-validator/pkg/api/validator"
	"github.com/golang/protobuf/jsonpb"
	structpb "github.com/golang/protobuf/ptypes/struct"
	cftypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Result is the result of reviewing an individual resource
type Result struct {
	// The name of the resource as given by CAI
	Name string
	// CAIResource is the resource as given by CAI
	CAIResource map[string]interface{}
	// ReviewResource is the resource sent to Constraint Framework for review.
	// For GCP types this is the unmodified resource from CAI, for K8S types, this is the unwrapped
	// resource.
	ReviewResource map[string]interface{}
	// ConstraintViolations are the constraints that were not satisfied during review.
	ConstraintViolations []ConstraintViolation
}

// NewResult creates a Result from the provided CF Response.
func NewResult(
	target string,
	caiResource map[string]interface{},
	reviewResource map[string]interface{},
	responses *cftypes.Responses) (*Result, error) {
	cfResponse, found := responses.ByTarget[target]
	if !found {
		return nil, errors.Errorf("No response for target %s", target)
	}

	resNameIface, found := caiResource["name"]
	if !found {
		return nil, errors.Errorf("result missing name field")
	}
	name, ok := resNameIface.(string)
	if !ok {
		return nil, errors.Errorf("failed to convert resource name to string %v", resNameIface)
	}

	result := &Result{
		Name:                 name,
		CAIResource:          caiResource,
		ReviewResource:       reviewResource,
		ConstraintViolations: make([]ConstraintViolation, len(cfResponse.Results)),
	}
	for idx, cfResult := range cfResponse.Results {
		result.ConstraintViolations[idx] = ConstraintViolation{
			Message:    cfResult.Msg,
			Metadata:   cfResult.Metadata,
			Constraint: cfResult.Constraint,
		}
	}
	return result, nil
}

// ConstraintViolations represents an unsatisfied constraint
type ConstraintViolation struct {
	// Message is a human readable message for the violation
	Message string
	// Metadata is the metadata returned by the constraint check
	Metadata map[string]interface{}
	// Constraint is the K8S resource of the constraint that triggered the violation
	Constraint *unstructured.Unstructured
}

// ToInsights returns the result represented as a slice of insights.
func (r *Result) ToInsights() []*Insight {
	if len(r.ConstraintViolations) == 0 {
		return nil
	}

	insights := make([]*Insight, len(r.ConstraintViolations))
	for idx, cv := range r.ConstraintViolations {
		insights[idx] = &Insight{
			Description:     cv.Message,
			TargetResources: []string{r.Name},
			InsightSubtype:  cv.Constraint.GetName(),
			Content: map[string]interface{}{
				"resource": r.CAIResource,
				"metadata": cv.Metadata,
			},
			Category: "SECURITY",
		}
	}
	return insights
}

func (r *Result) toViolations() ([]*validator.Violation, error) {
	var violations []*validator.Violation
	for _, rv := range r.ConstraintViolations {
		violation, err := rv.toViolation(r.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to convert result")
		}
		violations = append(violations, violation)
	}
	return violations, nil
}

func (v *ConstraintViolation) toViolation(name string) (*validator.Violation, error) {
	metadataJson, err := json.Marshal(v.Metadata)
	if err != nil {
		return nil, errors.Wrapf(
			err, "failed to marshal result metadata %v to json", v.Metadata)
	}
	metadata := &structpb.Value{}
	if err := jsonpb.UnmarshalString(string(metadataJson), metadata); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal json %s into structpb", string(metadataJson))
	}

	return &validator.Violation{
		Constraint: v.Constraint.GetName(),
		Resource:   name,
		Message:    v.Message,
		Metadata:   metadata,
	}, nil
}
