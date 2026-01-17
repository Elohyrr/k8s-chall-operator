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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

func TestBuildDeployment(t *testing.T) {
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
		Status: ctfv1alpha1.ChallengeInstanceStatus{
			Flags: []string{"FLAG{test_flag}"},
		},
	}

	challenge := &ctfv1alpha1.Challenge{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-challenge",
			Namespace: "ctf-instances",
		},
		Spec: ctfv1alpha1.ChallengeSpec{
			ID: "chall-1",
			Scenario: ctfv1alpha1.ChallengeScenarioSpec{
				Image:      "nginx:alpine",
				Port:       80,
				ExposeType: "NodePort",
				Env: []corev1.EnvVar{
					{Name: "CUSTOM_VAR", Value: "custom_value"},
				},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
			},
		},
	}

	deployment := BuildDeployment(instance, challenge)

	// Check deployment name
	expectedName := "test-instance-deployment"
	if deployment.Name != expectedName {
		t.Errorf("Expected deployment name %s, got %s", expectedName, deployment.Name)
	}

	// Check namespace
	if deployment.Namespace != "ctf-instances" {
		t.Errorf("Expected namespace ctf-instances, got %s", deployment.Namespace)
	}

	// Check labels
	if deployment.Labels["ctf.io/challenge"] != "chall-1" {
		t.Errorf("Expected challenge label chall-1, got %s", deployment.Labels["ctf.io/challenge"])
	}

	if deployment.Labels["ctf.io/instance"] != "test-instance" {
		t.Errorf("Expected instance label test-instance, got %s", deployment.Labels["ctf.io/instance"])
	}

	// Check container
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(deployment.Spec.Template.Spec.Containers))
	}

	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Image != "nginx:alpine" {
		t.Errorf("Expected image nginx:alpine, got %s", container.Image)
	}

	// Check port
	if len(container.Ports) != 1 || container.Ports[0].ContainerPort != 80 {
		t.Errorf("Expected port 80, got %v", container.Ports)
	}

	// Check environment variables include FLAG
	foundFlag := false
	foundCustom := false
	for _, env := range container.Env {
		if env.Name == "FLAG" && env.Value == "FLAG{test_flag}" {
			foundFlag = true
		}
		if env.Name == "CUSTOM_VAR" && env.Value == "custom_value" {
			foundCustom = true
		}
	}

	if !foundFlag {
		t.Error("Expected FLAG environment variable not found")
	}
	if !foundCustom {
		t.Error("Expected CUSTOM_VAR environment variable not found")
	}
}

func TestDeploymentName(t *testing.T) {
	instance := &ctfv1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
	}

	name := DeploymentName(instance)
	if name != "my-instance-deployment" {
		t.Errorf("Expected my-instance-deployment, got %s", name)
	}
}
