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
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

// BuildNetworkPolicy creates a NetworkPolicy for the AttackBox
// This isolates the attackbox so it can only communicate with:
// - Its own challenge (same instance)
// - DNS (kube-dns)
// - Internet (optional, excluding private ranges)
func BuildNetworkPolicy(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *networkingv1.NetworkPolicy {
	// Only create NetworkPolicy if AttackBox is enabled and NetworkPolicy is configured
	if challenge.Spec.Scenario.AttackBox == nil || !challenge.Spec.Scenario.AttackBox.Enabled {
		return nil
	}
	if challenge.Spec.Scenario.NetworkPolicy == nil || !challenge.Spec.Scenario.NetworkPolicy.Enabled {
		return nil
	}

	attackBoxName := AttackBoxDeploymentName(instance)
	policyName := NetworkPolicyName(instance)
	username := SanitizeForLabel(instance.Spec.SourceID)

	// Build egress rules
	egressRules := []networkingv1.NetworkPolicyEgressRule{}

	// Rule 1: Allow DNS (kube-dns in kube-system)
	if challenge.Spec.Scenario.NetworkPolicy.AllowDNS {
		port53 := intstr.FromInt32(53)
		udp := corev1.ProtocolUDP
		tcp := corev1.ProtocolTCP

		dnsRule := networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "kube-system",
						},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"k8s-app": "kube-dns",
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Protocol: &udp,
					Port:     &port53,
				},
				{
					Protocol: &tcp,
					Port:     &port53,
				},
			},
		}
		egressRules = append(egressRules, dnsRule)
	}

	// Rule 2: Allow access to the challenge of the same instance
	challengeRule := networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"ctf.io/instance": instance.Name,
						"app":             "challenge",
					},
				},
			},
		},
	}
	egressRules = append(egressRules, challengeRule)

	// Rule 3: Allow Internet access (excluding private ranges)
	if challenge.Spec.Scenario.NetworkPolicy.AllowInternet {
		internetRule := networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: "0.0.0.0/0",
						Except: []string{
							"10.0.0.0/8",     // Private range
							"172.16.0.0/12",  // Private range
							"192.168.0.0/16", // Private range
						},
					},
				},
			},
		}
		egressRules = append(egressRules, internetRule)
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"component":                    "attackbox",
				"ctf.io/challenge":             instance.Spec.ChallengeID,
				"ctf.io/instance":              instance.Name,
				"ctf.io/source":                username,
				"app.kubernetes.io/managed-by": "chall-operator",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": attackBoxName,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: egressRules,
		},
	}
}

// NetworkPolicyName returns the name of the network policy for an instance
func NetworkPolicyName(instance *ctfv1alpha1.ChallengeInstance) string {
	return instance.Name + "-attackbox-netpol"
}
