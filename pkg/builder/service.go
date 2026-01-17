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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

// BuildService creates a Service for a ChallengeInstance based on the Challenge template
func BuildService(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *corev1.Service {
	labels := map[string]string{
		"app":                          "challenge",
		"ctf.io/challenge":             instance.Spec.ChallengeID,
		"ctf.io/instance":              instance.Name,
		"ctf.io/source":                instance.Spec.SourceID,
		"app.kubernetes.io/name":       "challenge-instance",
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/managed-by": "chall-operator",
	}

	// Determine service type based on challenge config
	serviceType := corev1.ServiceTypeNodePort
	if challenge.Spec.Scenario.ExposeType == "LoadBalancer" {
		serviceType = corev1.ServiceTypeLoadBalancer
	}

	serviceName := instance.Name + "-svc"

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				"ctf.io/instance": instance.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "challenge",
					Port:       challenge.Spec.Scenario.Port,
					TargetPort: intstr.FromInt32(challenge.Spec.Scenario.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// ServiceName returns the name of the service for an instance
func ServiceName(instance *ctfv1alpha1.ChallengeInstance) string {
	return instance.Name + "-svc"
}

// GetConnectionInfo extracts connection information from a Service
// Returns a string like "nc <nodeIP> <nodePort>" for NodePort services
// or "nc <loadBalancerIP> <port>" for LoadBalancer services
func GetConnectionInfo(service *corev1.Service, nodeIP string) string {
	if service == nil || len(service.Spec.Ports) == 0 {
		return ""
	}

	port := service.Spec.Ports[0]

	switch service.Spec.Type {
	case corev1.ServiceTypeNodePort:
		if port.NodePort > 0 {
			return fmt.Sprintf("nc %s %d", nodeIP, port.NodePort)
		}
	case corev1.ServiceTypeLoadBalancer:
		if len(service.Status.LoadBalancer.Ingress) > 0 {
			ingress := service.Status.LoadBalancer.Ingress[0]
			host := ingress.IP
			if host == "" {
				host = ingress.Hostname
			}
			if host != "" {
				return fmt.Sprintf("nc %s %d", host, port.Port)
			}
		}
	}

	return ""
}
