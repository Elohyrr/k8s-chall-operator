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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

func TestBuildService_NodePort(t *testing.T) {
	instance := &ctfv1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "ctf-instances",
		},
		Spec: ctfv1alpha1.ChallengeInstanceSpec{
			ChallengeID:   "chall-1",
			SourceID:      "user-123",
			ChallengeName: "test-challenge",
		},
	}

	challenge := &ctfv1alpha1.Challenge{
		Spec: ctfv1alpha1.ChallengeSpec{
			ID: "chall-1",
			Scenario: ctfv1alpha1.ChallengeScenarioSpec{
				Image:      "nginx:alpine",
				Port:       8080,
				ExposeType: "NodePort",
			},
		},
	}

	service := BuildService(instance, challenge)

	// Check service name
	if service.Name != "test-instance-svc" {
		t.Errorf("Expected service name test-instance-svc, got %s", service.Name)
	}

	// Check service type
	if service.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("Expected ServiceTypeNodePort, got %s", service.Spec.Type)
	}

	// Check port
	if len(service.Spec.Ports) != 1 {
		t.Fatalf("Expected 1 port, got %d", len(service.Spec.Ports))
	}

	if service.Spec.Ports[0].Port != 8080 {
		t.Errorf("Expected port 8080, got %d", service.Spec.Ports[0].Port)
	}

	// Check selector
	if service.Spec.Selector["ctf.io/instance"] != "test-instance" {
		t.Errorf("Expected selector ctf.io/instance=test-instance, got %s", service.Spec.Selector["ctf.io/instance"])
	}
}

func TestBuildService_LoadBalancer(t *testing.T) {
	instance := &ctfv1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lb-instance",
			Namespace: "ctf-instances",
		},
		Spec: ctfv1alpha1.ChallengeInstanceSpec{
			ChallengeID: "chall-2",
			SourceID:    "team-1",
		},
	}

	challenge := &ctfv1alpha1.Challenge{
		Spec: ctfv1alpha1.ChallengeSpec{
			ID: "chall-2",
			Scenario: ctfv1alpha1.ChallengeScenarioSpec{
				Image:      "nginx:alpine",
				Port:       80,
				ExposeType: "LoadBalancer",
			},
		},
	}

	service := BuildService(instance, challenge)

	if service.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("Expected ServiceTypeLoadBalancer, got %s", service.Spec.Type)
	}
}

func TestServiceName(t *testing.T) {
	instance := &ctfv1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
	}

	name := ServiceName(instance)
	if name != "my-instance-svc" {
		t.Errorf("Expected my-instance-svc, got %s", name)
	}
}

func TestGetConnectionInfo_NodePort(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Port:     80,
					NodePort: 30080,
				},
			},
		},
	}

	connInfo := GetConnectionInfo(service, "192.168.1.100")
	expected := "nc 192.168.1.100 30080"
	if connInfo != expected {
		t.Errorf("Expected %s, got %s", expected, connInfo)
	}
}

func TestGetConnectionInfo_LoadBalancer(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "10.0.0.50"},
				},
			},
		},
	}

	connInfo := GetConnectionInfo(service, "ignored")
	expected := "nc 10.0.0.50 8080"
	if connInfo != expected {
		t.Errorf("Expected %s, got %s", expected, connInfo)
	}
}

func TestGetConnectionInfo_NoNodePort(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Port:     80,
					NodePort: 0, // Not yet assigned
				},
			},
		},
	}

	connInfo := GetConnectionInfo(service, "192.168.1.100")
	if connInfo != "" {
		t.Errorf("Expected empty string for unassigned NodePort, got %s", connInfo)
	}
}

func TestGetConnectionInfo_NilService(t *testing.T) {
	connInfo := GetConnectionInfo(nil, "192.168.1.100")
	if connInfo != "" {
		t.Errorf("Expected empty string for nil service, got %s", connInfo)
	}
}
