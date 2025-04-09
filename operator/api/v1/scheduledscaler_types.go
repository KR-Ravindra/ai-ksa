/*
Copyright 2025.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ScheduledScalerSpec defines the desired state of ScheduledScaler
type ScheduledScalerSpec struct {
	TargetDeploymentName      string `json:"targetDeploymentName"`
	TargetDeploymentNamespace string `json:"targetDeploymentNamespace"`
	Schedule                  string `json:"schedule,omitempty"`    // Cron expression
	Replicas                  int32  `json:"replicas"`              // Desired number of replicas
	StartTime                 string `json:"startTime,omitempty"`   // Optional field to specify start time
	OneTime                   bool   `json:"oneTime,omitempty"`     // Optional field to scale down once
	Duration                  string `json:"duration,omitempty"`    // Optional field to specify duration for scaling
	EndReplicas               int32  `json:"endReplicas,omitempty"` // Optional field to specify end replicas
}

// ScheduledScalerStatus defines the observed state of ScheduledScaler
type ScheduledScalerStatus struct {
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScheduledScaler is the Schema for the scheduledscalers API
type ScheduledScaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScheduledScalerSpec   `json:"spec,omitempty"`
	Status ScheduledScalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScheduledScalerList contains a list of ScheduledScaler
type ScheduledScalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScheduledScaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScheduledScaler{}, &ScheduledScalerList{})
}
