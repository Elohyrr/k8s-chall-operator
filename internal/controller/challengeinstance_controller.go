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

package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
	"github.com/leo/chall-operator/pkg/builder"
	"github.com/leo/chall-operator/pkg/flaggen"
)

// ChallengeInstanceReconciler reconciles a ChallengeInstance object
type ChallengeInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	NodeIP string // Node IP for connection info (set via env or config)
}

// +kubebuilder:rbac:groups=ctf.ctf.io,resources=challengeinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ctf.ctf.io,resources=challengeinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ctf.ctf.io,resources=challengeinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=ctf.ctf.io,resources=challenges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles the reconciliation loop for ChallengeInstance resources
func (r *ChallengeInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch the ChallengeInstance
	instance := &ctfv1alpha1.ChallengeInstance{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ChallengeInstance not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get ChallengeInstance")
		return ctrl.Result{}, err
	}

	// 2. Check expiry - delete if expired
	if instance.Spec.Until != nil && time.Now().After(instance.Spec.Until.Time) {
		log.Info("Instance expired, deleting", "instance", instance.Name)
		if err := r.Delete(ctx, instance); err != nil {
			log.Error(err, "Failed to delete expired instance")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 2b. Check if flag was validated - delete instance (janitor cleanup)
	if instance.Status.FlagValidated {
		log.Info("Flag validated, deleting instance", "instance", instance.Name)
		if err := r.Delete(ctx, instance); err != nil {
			log.Error(err, "Failed to delete validated instance")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 3. Fetch the Challenge template
	challenge := &ctfv1alpha1.Challenge{}
	challengeKey := types.NamespacedName{
		Name:      instance.Spec.ChallengeName,
		Namespace: instance.Namespace,
	}
	if err := r.Get(ctx, challengeKey, challenge); err != nil {
		log.Error(err, "Failed to get Challenge", "challengeName", instance.Spec.ChallengeName)
		instance.Status.Phase = "Failed"
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			log.Error(updateErr, "Failed to update instance status")
		}
		return ctrl.Result{}, err
	}

	// 4. Generate flag if not exists
	if len(instance.Status.Flags) == 0 {
		flag, err := flaggen.Generate(
			challenge.Spec.Scenario.FlagTemplate,
			instance.Name,
			instance.Spec.SourceID,
			instance.Spec.ChallengeID,
		)
		if err != nil {
			log.Error(err, "Failed to generate flag")
			return ctrl.Result{}, err
		}
		instance.Status.Flags = []string{flag}
		instance.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, instance); err != nil {
			log.Error(err, "Failed to update instance status with flag")
			return ctrl.Result{}, err
		}
		// Requeue to continue with deployment creation
		return ctrl.Result{Requeue: true}, nil
	}

	// 5. Create or Update Deployment
	deployment := builder.BuildDeployment(instance, challenge)
	if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference on Deployment")
		return ctrl.Result{}, err
	}

	existingDeployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, existingDeployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating Deployment", "deployment", deployment.Name)
			if err := r.Create(ctx, deployment); err != nil {
				log.Error(err, "Failed to create Deployment")
				return ctrl.Result{}, err
			}
			instance.Status.DeploymentName = deployment.Name
			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update instance status with deployment name")
				return ctrl.Result{}, err
			}
		} else {
			log.Error(err, "Failed to get Deployment")
			return ctrl.Result{}, err
		}
	}

	// 6. Create or Update Service
	service := builder.BuildService(instance, challenge)
	if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference on Service")
		return ctrl.Result{}, err
	}

	existingService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, existingService)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating Service", "service", service.Name)
			if err := r.Create(ctx, service); err != nil {
				log.Error(err, "Failed to create Service")
				return ctrl.Result{}, err
			}
			instance.Status.ServiceName = service.Name
			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update instance status with service name")
				return ctrl.Result{}, err
			}
		} else {
			log.Error(err, "Failed to get Service")
			return ctrl.Result{}, err
		}
	} else {
		// Service exists, update connection info if NodePort is assigned
		connInfo := builder.GetConnectionInfo(existingService, r.getNodeIP())
		if connInfo != "" && instance.Status.ConnectionInfo != connInfo {
			instance.Status.ConnectionInfo = connInfo
			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update connection info")
				return ctrl.Result{}, err
			}
		}
	}

	// 7. Create AttackBox Deployment if enabled
	if attackBoxDeploy := builder.BuildAttackBoxDeployment(instance, challenge); attackBoxDeploy != nil {
		if err := controllerutil.SetControllerReference(instance, attackBoxDeploy, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference on AttackBox Deployment")
			return ctrl.Result{}, err
		}

		existingAttackBox := &appsv1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{Name: attackBoxDeploy.Name, Namespace: attackBoxDeploy.Namespace}, existingAttackBox)
		if err != nil && apierrors.IsNotFound(err) {
			log.Info("Creating AttackBox Deployment", "deployment", attackBoxDeploy.Name)
			if err := r.Create(ctx, attackBoxDeploy); err != nil {
				log.Error(err, "Failed to create AttackBox Deployment")
				return ctrl.Result{}, err
			}
		}
	}

	// 8. Create AttackBox Service if enabled
	if attackBoxSvc := builder.BuildAttackBoxService(instance, challenge); attackBoxSvc != nil {
		if err := controllerutil.SetControllerReference(instance, attackBoxSvc, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference on AttackBox Service")
			return ctrl.Result{}, err
		}

		existingAttackBoxSvc := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{Name: attackBoxSvc.Name, Namespace: attackBoxSvc.Namespace}, existingAttackBoxSvc)
		if err != nil && apierrors.IsNotFound(err) {
			log.Info("Creating AttackBox Service", "service", attackBoxSvc.Name)
			if err := r.Create(ctx, attackBoxSvc); err != nil {
				log.Error(err, "Failed to create AttackBox Service")
				return ctrl.Result{}, err
			}
		}
	}

	// 9. Create Ingress if enabled
	if ingress := builder.BuildIngress(instance, challenge); ingress != nil {
		if err := controllerutil.SetControllerReference(instance, ingress, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference on Ingress")
			return ctrl.Result{}, err
		}

		existingIngress := &networkingv1.Ingress{}
		err := r.Get(ctx, types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}, existingIngress)
		if err != nil && apierrors.IsNotFound(err) {
			log.Info("Creating Ingress", "ingress", ingress.Name)
			if err := r.Create(ctx, ingress); err != nil {
				log.Error(err, "Failed to create Ingress")
				return ctrl.Result{}, err
			}
			// Update connection info with Ingress hostname
			hostname := builder.GetIngressHostname(instance, challenge)
			if hostname != "" {
				instance.Status.ConnectionInfo = fmt.Sprintf("http://%s", hostname)
				if challenge.Spec.Scenario.AttackBox != nil && challenge.Spec.Scenario.AttackBox.Enabled {
					instance.Status.ConnectionInfo = fmt.Sprintf("Challenge: http://%s\nTerminal: http://%s/terminal", hostname, hostname)
				}
			}
		}
	}

	// 10. Create NetworkPolicy if enabled
	if netpol := builder.BuildNetworkPolicy(instance, challenge); netpol != nil {
		if err := controllerutil.SetControllerReference(instance, netpol, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference on NetworkPolicy")
			return ctrl.Result{}, err
		}

		existingNetpol := &networkingv1.NetworkPolicy{}
		err := r.Get(ctx, types.NamespacedName{Name: netpol.Name, Namespace: netpol.Namespace}, existingNetpol)
		if err != nil && apierrors.IsNotFound(err) {
			log.Info("Creating NetworkPolicy", "networkpolicy", netpol.Name)
			if err := r.Create(ctx, netpol); err != nil {
				log.Error(err, "Failed to create NetworkPolicy")
				return ctrl.Result{}, err
			}
		}
	}

	// 11. Check if Deployment is ready
	if existingDeployment.Name != "" {
		if err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, existingDeployment); err == nil {
			if existingDeployment.Status.ReadyReplicas > 0 {
				if instance.Status.Phase != "Running" || !instance.Status.Ready {
					instance.Status.Phase = "Running"
					instance.Status.Ready = true

					// Update connection info from service
					if err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, existingService); err == nil {
						connInfo := builder.GetConnectionInfo(existingService, r.getNodeIP())
						if connInfo != "" {
							instance.Status.ConnectionInfo = connInfo
						}
					}

					if err := r.Status().Update(ctx, instance); err != nil {
						log.Error(err, "Failed to update instance status to Running")
						return ctrl.Result{}, err
					}
					log.Info("Instance is now Running", "instance", instance.Name, "connectionInfo", instance.Status.ConnectionInfo)
				}
			}
		}
	}

	// 8. Requeue to check status periodically
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// getNodeIP returns the node IP for connection info
func (r *ChallengeInstanceReconciler) getNodeIP() string {
	if r.NodeIP != "" {
		return r.NodeIP
	}
	// Try to get from environment
	if nodeIP := os.Getenv("NODE_IP"); nodeIP != "" {
		return nodeIP
	}
	// Default fallback
	return "localhost"
}

// SetupWithManager sets up the controller with the Manager.
func (r *ChallengeInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ctfv1alpha1.ChallengeInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("challengeinstance").
		Complete(r)
}

// Helper function to create a pointer to a string
func ptr[T any](v T) *T {
	return &v
}

// formatConnectionInfo creates a connection string based on service type
func formatConnectionInfo(service *corev1.Service, nodeIP string) string {
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
