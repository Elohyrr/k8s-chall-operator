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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

// BuildAttackBoxDeployment creates a Deployment for the AttackBox (web terminal)
// The AttackBox includes an auth-proxy sidecar and the ttyd terminal container
func BuildAttackBoxDeployment(
	instance *ctfv1alpha1.ChallengeInstance,
	challenge *ctfv1alpha1.Challenge
	) *appsv1.Deployment {
	if challenge.Spec.Scenario.AttackBox == nil || !challenge.Spec.Scenario.AttackBox.Enabled {
		return nil
	}

	attackBoxName := AttackBoxDeploymentName(instance)
	username := SanitizeForLabel(instance.Spec.SourceID)

	labels := map[string]string{
		"app":                          attackBoxName,
		"component":                    "attackbox",
		"ctf.io/challenge":             instance.Spec.ChallengeID,
		"ctf.io/instance":              instance.Name,
		"ctf.io/source":                username,
		"app.kubernetes.io/name":       "attackbox",
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/managed-by": "chall-operator",
	}

	// AttackBox image and port
	attackBoxImage := "attack-box:latest"
	if challenge.Spec.Scenario.AttackBox.Image != "" {
		attackBoxImage = challenge.Spec.Scenario.AttackBox.Image
	}

	ttydPort := int32(7681)
	if challenge.Spec.Scenario.AttackBox.Port > 0 {
		ttydPort = challenge.Spec.Scenario.AttackBox.Port
	}

	// Challenge service DNS name for attackbox to connect to
	challengeSvcDNS := fmt.Sprintf("%s.%s.svc.cluster.local", ServiceName(instance), instance.Namespace)

	containers := []corev1.Container{}

	// Auth proxy sidecar for attackbox (if AuthProxy is enabled globally)
	if challenge.Spec.Scenario.AuthProxy != nil && challenge.Spec.Scenario.AuthProxy.Enabled {
		authProxyImage := "ctf-auth-proxy:simple"
		if challenge.Spec.Scenario.AuthProxy.Image != "" {
			authProxyImage = challenge.Spec.Scenario.AuthProxy.Image
		}

		authProxyContainer := corev1.Container{
			Name:            "auth-proxy-attackbox",
			Image:           authProxyImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env: []corev1.EnvVar{
				{
					Name:  "ALLOWED_USER",
					Value: instance.Spec.SourceID,
				},
				{
					Name:  "TARGET_PORT",
					Value: fmt.Sprintf("%d", ttydPort),
				},
				{
					Name:  "LISTEN_PORT",
					Value: "8888",
				},
			},
			Ports: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 8888,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			Resources: challenge.Spec.Scenario.AuthProxy.Resources,
		}
		containers = append(containers, authProxyContainer)
	}

	// AttackBox container (ttyd terminal)
	attackBoxContainer := corev1.Container{
		Name:            "attackbox",
		Image:           attackBoxImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{
				Name:  "PS1",
				Value: fmt.Sprintf("\\[\\e[1;32m\\]%s@attackbox\\[\\e[0m\\]:\\[\\e[1;34m\\]\\w\\[\\e[0m\\]$ ", username),
			},
			{
				Name:  "CHALLENGE_HOST",
				Value: challengeSvcDNS,
			},
			{
				Name:  "TTYD_PORT",
				Value: fmt.Sprintf("%d", ttydPort),
			},
			{
				Name:  "INSTANCE_ID",
				Value: instance.Name,
			},
			{
				Name:  "SOURCE_ID",
				Value: instance.Spec.SourceID,
			},
			{
				Name:  "CHALLENGE_ID",
				Value: instance.Spec.ChallengeID,
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "ttyd",
				ContainerPort: ttydPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources: challenge.Spec.Scenario.AttackBox.Resources,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			RunAsUser:                ptr.To(int64(1000)),
			AllowPrivilegeEscalation: ptr.To(false),
		},
	}
	containers = append(containers, attackBoxContainer)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      attackBoxName,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": attackBoxName,
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

// BuildAttackBoxService creates a Service for the AttackBox
func BuildAttackBoxService(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *corev1.Service {
	if challenge.Spec.Scenario.AttackBox == nil || !challenge.Spec.Scenario.AttackBox.Enabled {
		return nil
	}

	attackBoxName := AttackBoxDeploymentName(instance)
	serviceName := AttackBoxServiceName(instance)

	// Determine target port based on auth proxy
	targetPort := int32(7681)
	if challenge.Spec.Scenario.AttackBox.Port > 0 {
		targetPort = challenge.Spec.Scenario.AttackBox.Port
	}

	// If auth proxy is enabled, target port 8888 (auth-proxy), otherwise ttyd port
	serviceTargetPort := targetPort
	if challenge.Spec.Scenario.AuthProxy != nil && challenge.Spec.Scenario.AuthProxy.Enabled {
		serviceTargetPort = 8888
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                          attackBoxName,
				"component":                    "attackbox",
				"ctf.io/challenge":             instance.Spec.ChallengeID,
				"ctf.io/instance":              instance.Name,
				"app.kubernetes.io/managed-by": "chall-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": attackBoxName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(serviceTargetPort),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// AttackBoxDeploymentName returns the name of the attackbox deployment for an instance
func AttackBoxDeploymentName(instance *ctfv1alpha1.ChallengeInstance) string {
	return instance.Name + "-attackbox"
}

// AttackBoxServiceName returns the name of the attackbox service for an instance
func AttackBoxServiceName(instance *ctfv1alpha1.ChallengeInstance) string {
	return instance.Name + "-attackbox-svc"
}
