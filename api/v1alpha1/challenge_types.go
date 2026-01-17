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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChallengeSpec defines the desired state of Challenge
type ChallengeSpec struct {
	// ID is the unique identifier for this challenge (used by CTFd)
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Scenario defines how to deploy the challenge
	// +kubebuilder:validation:Required
	Scenario ChallengeScenarioSpec `json:"scenario"`

	// Timeout in seconds before instance expires (default: 600)
	// +kubebuilder:default=600
	// +optional
	Timeout int64 `json:"timeout,omitempty"`
}

// ChallengeScenarioSpec defines the container configuration for a challenge
type ChallengeScenarioSpec struct {
	// Image is the container image to deploy
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// Port is the container port to expose
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// ExposeType defines how to expose the service (NodePort, LoadBalancer, or Ingress)
	// +kubebuilder:validation:Enum=NodePort;LoadBalancer;Ingress
	// +kubebuilder:default=NodePort
	// +optional
	ExposeType string `json:"exposeType,omitempty"`

	// Env is a list of environment variables to set in the container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// FlagTemplate is a Go template for generating unique flags per instance
	// Available variables: .InstanceID, .SourceID, .ChallengeID, .RandomString
	// Example: "FLAG{{{.ChallengeID}}_{{.SourceID}}_{{.RandomString}}}"
	// +optional
	FlagTemplate string `json:"flagTemplate,omitempty"`

	// Resources defines the resource requirements for the container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// AuthProxy enables the auth-proxy sidecar to verify user identity
	// +optional
	AuthProxy *AuthProxySpec `json:"authProxy,omitempty"`

	// AttackBox enables an attack box (web terminal) for this challenge
	// +optional
	AttackBox *AttackBoxSpec `json:"attackBox,omitempty"`

	// Ingress configuration for exposing via Ingress controller
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// NetworkPolicy enables network isolation for the challenge
	// +optional
	NetworkPolicy *NetworkPolicySpec `json:"networkPolicy,omitempty"`
}

// AuthProxySpec defines the auth-proxy sidecar configuration
type AuthProxySpec struct {
	// Enabled enables the auth-proxy sidecar
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Image is the auth-proxy container image
	// +kubebuilder:default="ctf-auth-proxy:simple"
	// +optional
	Image string `json:"image,omitempty"`

	// Resources for the auth-proxy sidecar
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// AttackBoxSpec defines the attack box configuration
type AttackBoxSpec struct {
	// Enabled enables the attack box deployment
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Image is the attack box container image
	// +kubebuilder:default="attack-box:latest"
	// +optional
	Image string `json:"image,omitempty"`

	// Port is the ttyd port (default: 7681)
	// +kubebuilder:default=7681
	// +optional
	Port int32 `json:"port,omitempty"`

	// Resources for the attack box container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// IngressSpec defines the Ingress configuration
type IngressSpec struct {
	// Enabled enables Ingress creation
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// HostTemplate is a Go template for generating the hostname
	// Available variables: .InstanceName, .Username, .ChallengeID
	// Example: "ctf.{{.InstanceName}}.{{.Username}}.{{.ChallengeID}}.devleo.local"
	// +optional
	HostTemplate string `json:"hostTemplate,omitempty"`

	// IngressClassName is the ingress class to use
	// +kubebuilder:default="nginx"
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`

	// Annotations to add to the Ingress
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS enables TLS for the Ingress
	// +optional
	TLS bool `json:"tls,omitempty"`

	// ClusterIssuer for cert-manager TLS
	// +optional
	ClusterIssuer string `json:"clusterIssuer,omitempty"`
}

// NetworkPolicySpec defines network isolation rules
type NetworkPolicySpec struct {
	// Enabled enables NetworkPolicy creation
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// AllowInternet allows egress to internet (excluding private ranges)
	// +kubebuilder:default=true
	// +optional
	AllowInternet bool `json:"allowInternet,omitempty"`

	// AllowDNS allows egress to kube-dns
	// +kubebuilder:default=true
	// +optional
	AllowDNS bool `json:"allowDNS,omitempty"`
}

// ChallengeStatus defines the observed state of Challenge
type ChallengeStatus struct {
	// ActiveInstances is the number of currently running instances
	// +optional
	ActiveInstances int32 `json:"activeInstances,omitempty"`

	// Conditions represent the current state of the Challenge
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Challenge is the Schema for the challenges API
type Challenge struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Challenge
	// +required
	Spec ChallengeSpec `json:"spec"`

	// status defines the observed state of Challenge
	// +optional
	Status ChallengeStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChallengeList contains a list of Challenge
type ChallengeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Challenge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Challenge{}, &ChallengeList{})
}
