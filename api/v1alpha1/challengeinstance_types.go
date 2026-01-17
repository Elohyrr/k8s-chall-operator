/*
Copyright 2026.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChallengeInstanceSpec defines the desired state of ChallengeInstance
type ChallengeInstanceSpec struct {
	// ChallengeID is the ID of the challenge (matches Challenge.Spec.ID)
	// +kubebuilder:validation:Required
	ChallengeID string `json:"challengeId"`

	// SourceID is the user or team identifier
	// +kubebuilder:validation:Required
	SourceID string `json:"sourceId"`

	// ChallengeName is the name of the Challenge CRD to reference
	// +kubebuilder:validation:Required
	ChallengeName string `json:"challengeName"`

	// Additional is a map of extra configuration passed from CTFd
	// +optional
	Additional map[string]string `json:"additional,omitempty"`

	// Since is the time when the instance was created
	// +kubebuilder:validation:Required
	Since metav1.Time `json:"since"`

	// Until is the time when the instance will expire
	// +optional
	Until *metav1.Time `json:"until,omitempty"`
}

// ChallengeInstanceStatus defines the observed state of ChallengeInstance
type ChallengeInstanceStatus struct {
	// Phase represents the current lifecycle phase (Pending, Running, Failed)
	// +kubebuilder:validation:Enum=Pending;Running;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// ConnectionInfo is the connection string shown to users (e.g., "nc host port")
	// +optional
	ConnectionInfo string `json:"connectionInfo,omitempty"`

	// Flags contains the generated flags for this instance
	// +optional
	Flags []string `json:"flags,omitempty"`

	// DeploymentName is the name of the created Deployment
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// ServiceName is the name of the created Service
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// Ready indicates if the instance is fully operational
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Conditions represent the current state of the ChallengeInstance
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ChallengeInstance is the Schema for the challengeinstances API
type ChallengeInstance struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ChallengeInstance
	// +required
	Spec ChallengeInstanceSpec `json:"spec"`

	// status defines the observed state of ChallengeInstance
	// +optional
	Status ChallengeInstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChallengeInstanceList contains a list of ChallengeInstance
type ChallengeInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChallengeInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ChallengeInstance{}, &ChallengeInstanceList{})
}
