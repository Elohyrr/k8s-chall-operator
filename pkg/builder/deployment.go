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

package builder

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

// SanitizeForLabel converts a string to be DNS-safe for Kubernetes labels
// Example: "uwu@uwu.uwu" -> "uwu-at-uwu-uwu"
func SanitizeForLabel(s string) string {
	result := strings.ReplaceAll(s, "@", "-at-")
	result = strings.ReplaceAll(result, ".", "-")
	result = strings.ToLower(result)
	// Truncate to 63 chars (K8s label limit)
	if len(result) > 63 {
		result = result[:63]
	}
	return result
}

// BuildDeployment creates a Deployment for a ChallengeInstance based on the Challenge template
// If AuthProxy is enabled, adds a sidecar container that verifies user identity
func BuildDeployment(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *appsv1.Deployment {
	labels := map[string]string{
		"app":                          "challenge",
		"ctf.io/challenge":             instance.Spec.ChallengeID,
		"ctf.io/instance":              instance.Name,
		"ctf.io/source":                SanitizeForLabel(instance.Spec.SourceID),
		"app.kubernetes.io/name":       "challenge-instance",
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/managed-by": "chall-operator",
	}

	// Copy environment variables from challenge spec
	env := make([]corev1.EnvVar, len(challenge.Spec.Scenario.Env))
	copy(env, challenge.Spec.Scenario.Env)

	// Inject flag into environment if available
	if len(instance.Status.Flags) > 0 {
		env = append(env, corev1.EnvVar{
			Name:  "FLAG",
			Value: instance.Status.Flags[0],
		})
	}

	// Inject instance metadata as environment variables
	env = append(env,
		corev1.EnvVar{
			Name:  "INSTANCE_ID",
			Value: instance.Name,
		},
		corev1.EnvVar{
			Name:  "SOURCE_ID",
			Value: instance.Spec.SourceID,
		},
		corev1.EnvVar{
			Name:  "CHALLENGE_ID",
			Value: instance.Spec.ChallengeID,
		},
	)

	deploymentName := DeploymentName(instance)

	// Build containers list
	containers := []corev1.Container{}

	// Check if AuthProxy is enabled
	authProxyEnabled := challenge.Spec.Scenario.AuthProxy != nil && challenge.Spec.Scenario.AuthProxy.Enabled
	challengePort := challenge.Spec.Scenario.Port

	if authProxyEnabled {
		// Auth proxy listens on port 80, forwards to challenge port
		authProxyImage := "ctf-auth-proxy:simple"
		if challenge.Spec.Scenario.AuthProxy.Image != "" {
			authProxyImage = challenge.Spec.Scenario.AuthProxy.Image
		}

		authProxyContainer := corev1.Container{
			Name:            "auth-proxy",
			Image:           authProxyImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env: []corev1.EnvVar{
				{
					Name:  "ALLOWED_USER",
					Value: instance.Spec.SourceID, // Original email/ID for verification
				},
				{
					Name:  "TARGET_PORT",
					Value: fmt.Sprintf("%d", challengePort),
				},
			},
			Ports: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 80,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			Resources: challenge.Spec.Scenario.AuthProxy.Resources,
		}
		containers = append(containers, authProxyContainer)
	}

	// Main challenge container
	challengeContainer := corev1.Container{
		Name:            "challenge",
		Image:           challenge.Spec.Scenario.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				Name:          "challenge",
				ContainerPort: challengePort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:       env,
		Resources: challenge.Spec.Scenario.Resources,
	}
	containers = append(containers, challengeContainer)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"ctf.io/instance": instance.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers:    containers,
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}
}

// DeploymentName returns the name of the deployment for an instance
func DeploymentName(instance *ctfv1alpha1.ChallengeInstance) string {
	return instance.Name + "-deployment"
}
