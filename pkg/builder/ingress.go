/* (same license header) */
package builder

import (
	"bytes"
	"fmt"
	"os"
	"text/template"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

// getDefaultHostTemplate returns the default host template from env or fallback
func getDefaultHostTemplate() string {
	if hostTemplate := os.Getenv("DEFAULT_HOST_TEMPLATE"); hostTemplate != "" {
		return hostTemplate
	}
	return "ctf.{{.InstanceName}}.{{.Username}}.{{.ChallengeID}}.devleo.local"
}

// getAuthURL returns the auth URL from env or fallback
func getAuthURL() string {
	if authURL := os.Getenv("AUTH_URL"); authURL != "" {
		return authURL
	}
	return "auth.devleo.local"
}

// Shorter constants for long annotation values (avoid lll >120 chars)

// HostContext contains variables available for host template rendering
type HostContext struct {
	InstanceName string
	Username     string
	ChallengeID  string
	SourceID     string
}

// BuildIngress creates an Ingress for a ChallengeInstance
// The Ingress exposes both the challenge (/) and attackbox (/terminal) paths
func BuildIngress(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *networkingv1.Ingress {
	if challenge.Spec.Scenario.Ingress == nil || !challenge.Spec.Scenario.Ingress.Enabled {
		return nil
	}

	ingressName := IngressName(instance)
	username := SanitizeForLabel(instance.Spec.SourceID)

	// Generate hostname from template
	hostTemplate := getDefaultHostTemplate()
	if challenge.Spec.Scenario.Ingress.HostTemplate != "" {
		hostTemplate = challenge.Spec.Scenario.Ingress.HostTemplate
	}

	hostname, err := renderHostTemplate(hostTemplate, HostContext{
		InstanceName: instance.Name,
		Username:     username,
		ChallengeID:  instance.Spec.ChallengeID,
		SourceID:     instance.Spec.SourceID,
	})
	if err != nil {
		// Fallback to simple hostname
		hostname = instance.Name + ".ctf.local"
	}

	// Build annotations
	annotations := map[string]string{
		"kubernetes.io/ingress.class": challenge.Spec.Scenario.Ingress.IngressClassName,
	}

	// Default OAuth2 annotations for CTF authentication
	authURL := getAuthURL()
	oauthURL := "http://oauth2-proxy.keycloak.svc.cluster.local:4180/oauth2/auth"
	authSignin := fmt.Sprintf("http://%s/oauth2/start?rd=$scheme://$host$request_uri", authURL)
	responseHeaders := "X-Auth-Request-User,X-Auth-Request-Email,Authorization"
	defaultAnnotations := map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect":            "false",
		"nginx.ingress.kubernetes.io/auth-url":                oauthURL,
		"nginx.ingress.kubernetes.io/auth-signin":             authSignin,
		"nginx.ingress.kubernetes.io/auth-response-headers":   responseHeaders,
		"nginx.ingress.kubernetes.io/proxy-buffer-size":       "16k",
		"nginx.ingress.kubernetes.io/proxy-buffers-number":    "4",
		"nginx.ingress.kubernetes.io/proxy-busy-buffers-size": "24k",
	}

	// Add websocket support if attackbox is enabled
	if challenge.Spec.Scenario.AttackBox != nil && challenge.Spec.Scenario.AttackBox.Enabled {
		defaultAnnotations["nginx.ingress.kubernetes.io/proxy-read-timeout"] = "3600"
		defaultAnnotations["nginx.ingress.kubernetes.io/proxy-send-timeout"] = "3600"
		defaultAnnotations["nginx.ingress.kubernetes.io/websocket-services"] = AttackBoxServiceName(instance)
		// Use regex paths with rewrite to strip /terminal prefix for attackbox
		defaultAnnotations["nginx.ingress.kubernetes.io/use-regex"] = "true"
		defaultAnnotations["nginx.ingress.kubernetes.io/rewrite-target"] = "/$2"
	}

	// Merge default annotations
	for k, v := range defaultAnnotations {
		if _, exists := annotations[k]; !exists {
			annotations[k] = v
		}
	}

	// Merge custom annotations from spec
	for k, v := range challenge.Spec.Scenario.Ingress.Annotations {
		annotations[k] = v
	}

	// Add TLS annotations if enabled
	if challenge.Spec.Scenario.Ingress.TLS && challenge.Spec.Scenario.Ingress.ClusterIssuer != "" {
		annotations["cert-manager.io/cluster-issuer"] = challenge.Spec.Scenario.Ingress.ClusterIssuer
	}

	// Build paths
	pathTypePrefix := networkingv1.PathTypePrefix
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific

	var paths []networkingv1.HTTPIngressPath

	// Add attackbox path if enabled (must come first for regex matching)
	if challenge.Spec.Scenario.AttackBox != nil && challenge.Spec.Scenario.AttackBox.Enabled {
		// Use regex to capture and rewrite /terminal/* to /*
		paths = append(paths, networkingv1.HTTPIngressPath{
			Path:     "/terminal(/|$)(.*)",
			PathType: &pathTypeImplementationSpecific,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: AttackBoxServiceName(instance),
					Port: networkingv1.ServiceBackendPort{
						Number: 8080,
					},
				},
			},
		})
	}

	// Challenge path (/) - catches everything else
	paths = append(paths, networkingv1.HTTPIngressPath{
		Path:     "/",
		PathType: &pathTypePrefix,
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: ServiceName(instance),
				Port: networkingv1.ServiceBackendPort{
					Number: 80,
				},
			},
		},
	})

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingressName,
			Namespace:   instance.Namespace,
			Annotations: annotations,
			Labels: map[string]string{
				"ctf.io/challenge":             instance.Spec.ChallengeID,
				"ctf.io/instance":              instance.Name,
				"ctf.io/source":                username,
				"app.kubernetes.io/managed-by": "chall-operator",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: hostname,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}

	// Add TLS if enabled
	if challenge.Spec.Scenario.Ingress.TLS {
		ingress.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{hostname},
				SecretName: ingressName + "-tls",
			},
		}
	}

	return ingress
}

// IngressName returns the name of the ingress for an instance
func IngressName(instance *ctfv1alpha1.ChallengeInstance) string {
	return instance.Name + "-ingress"
}

// GetIngressHostname returns the hostname for an instance's ingress
func GetIngressHostname(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) string {
	if challenge.Spec.Scenario.Ingress == nil {
		return ""
	}

	hostTemplate := getDefaultHostTemplate()
	if challenge.Spec.Scenario.Ingress.HostTemplate != "" {
		hostTemplate = challenge.Spec.Scenario.Ingress.HostTemplate
	}

	hostname, err := renderHostTemplate(hostTemplate, HostContext{
		InstanceName: instance.Name,
		Username:     SanitizeForLabel(instance.Spec.SourceID),
		ChallengeID:  instance.Spec.ChallengeID,
		SourceID:     instance.Spec.SourceID,
	})
	if err != nil {
		return instance.Name + ".ctf.local"
	}
	return hostname
}

// renderHostTemplate renders a hostname template with the given context
func renderHostTemplate(tmpl string, ctx HostContext) (string, error) {
	t, err := template.New("host").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", err
	}

	return buf.String(), nil
}
