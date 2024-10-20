/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const IstioKind = "Istio"

// IstioSpec defines the desired state of Istio
type IstioSpec struct {
	// Version defines the version of Istio to install. If not specified, the
	// latest version supported by the operator is installed.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1,displayName="Istio Version",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:fieldGroup:General","urn:alm:descriptor:com.tectonic.ui:select:v3.0"}
	Version string `json:"version,omitempty"`

	// The built-in installation configuration profile to use.
	// When this field is left empty, the 'default' profile will be used.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Profile"
	Profile string `json:"profile,omitempty"`

	// Values defines the values to be passed to the Helm chart when installing Istio.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Helm Values"
	Values json.RawMessage `json:"values,omitempty"`
}

func (s *IstioSpec) GetValues() map[string]interface{} {
	var vals map[string]interface{}
	err := json.Unmarshal(s.Values, &vals)
	if err != nil {
		return nil
	}
	return vals
}

func (s *IstioSpec) SetValues(values map[string]interface{}) error {
	jsonVals, err := json.Marshal(values)
	if err != nil {
		return err
	}
	s.Values = jsonVals
	return nil
}

// IstioStatus defines the observed state of Istio
type IstioStatus struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Applied Helm Values"
	AppliedValues json.RawMessage `json:"appliedValues,omitempty"`

	// ObservedGeneration is the most recent generation observed for this
	// Istio object. It corresponds to the object's generation, which is
	// updated on mutation by the API Server. The information in the status
	// pertains to this particular generation of the object.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Represents the latest available observations of the object's current state.
	Conditions []IstioCondition `json:"conditions,omitempty"`

	// Reports the current state of the object.
	State IstioConditionReason `json:"state,omitempty"`
}

func (s *IstioStatus) GetAppliedValues() map[string]interface{} {
	var vals map[string]interface{}
	err := json.Unmarshal(s.AppliedValues, &vals)
	if err != nil {
		return nil
	}
	return vals
}

// GetCondition returns the condition of the specified type
func (s *IstioStatus) GetCondition(conditionType IstioConditionType) IstioCondition {
	if s != nil {
		for i := range s.Conditions {
			if s.Conditions[i].Type == conditionType {
				return s.Conditions[i]
			}
		}
	}
	return IstioCondition{Type: conditionType, Status: metav1.ConditionUnknown}
}

// testTime is only in unit tests to pin the time to a fixed value
var testTime *time.Time

// SetCondition sets a specific condition in the list of conditions
func (s *IstioStatus) SetCondition(condition IstioCondition) {
	var now time.Time
	if testTime == nil {
		now = time.Now()
	} else {
		now = *testTime
	}

	// The lastTransitionTime only gets serialized out to the second.  This can
	// break update skipping, as the time in the resource returned from the client
	// may not match the time in our cached status during a reconcile.  We truncate
	// here to save any problems down the line.
	lastTransitionTime := metav1.NewTime(now.Truncate(time.Second))

	for i, prevCondition := range s.Conditions {
		if prevCondition.Type == condition.Type {
			if prevCondition.Status != condition.Status {
				condition.LastTransitionTime = lastTransitionTime
			} else {
				condition.LastTransitionTime = prevCondition.LastTransitionTime
			}
			s.Conditions[i] = condition
			return
		}
	}

	// If the condition does not exist, initialize the lastTransitionTime
	condition.LastTransitionTime = lastTransitionTime
	s.Conditions = append(s.Conditions, condition)
}

// A Condition represents a specific observation of the object's state.
type IstioCondition struct {
	// The type of this condition.
	Type IstioConditionType `json:"type,omitempty"`

	// The status of this condition. Can be True, False or Unknown.
	Status metav1.ConditionStatus `json:"status,omitempty"`

	// Unique, single-word, CamelCase reason for the condition's last transition.
	Reason IstioConditionReason `json:"reason,omitempty"`

	// Human-readable message indicating details about the last transition.
	Message string `json:"message,omitempty"`

	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// IstioConditionType represents the type of the condition.  Condition stages are:
// Installed, Reconciled, Ready
type IstioConditionType string

// IstioConditionReason represents a short message indicating how the condition came
// to be in its present state.
type IstioConditionReason string

const (
	// ConditionTypeReconciled signifies whether the controller has
	// successfully reconciled the resources defined through the CR.
	ConditionTypeReconciled IstioConditionType = "Reconciled"

	// ConditionReasonReconcileError indicates that the reconciliation of the resource has failed, but will be retried.
	ConditionReasonReconcileError IstioConditionReason = "ReconcileError"
)

const (
	// ConditionTypeReady signifies whether any Deployment, StatefulSet,
	// etc. resources are Ready.
	ConditionTypeReady IstioConditionType = "Ready"

	// ConditionReasonIstiodNotReady indicates that the control plane is fully reconciled, but istiod is not ready.
	ConditionReasonIstiodNotReady IstioConditionReason = "IstiodNotReady"

	// ConditionReasonCNINotReady indicates that the control plane is fully reconciled, but istio-cni-node is not ready.
	ConditionReasonCNINotReady IstioConditionReason = "CNINotReady"
)

const (
	// ConditionReasonHealthy indicates that the control plane is fully reconciled and that all components are ready.
	ConditionReasonHealthy IstioConditionReason = "Healthy"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Whether the control plane installation is ready to handle requests."
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.state",description="The current state of this object."
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of the control plane installation."
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the object"

// Istio represents an Istio Service Mesh deployment
type Istio struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IstioSpec   `json:"spec,omitempty"`
	Status IstioStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IstioList contains a list of Istio
type IstioList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Istio `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Istio{}, &IstioList{})
}
