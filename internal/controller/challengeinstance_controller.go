/* (same license header) */
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

	// Ensure Deployment
	if res, err := r.ensureDeployment(ctx, instance, challenge); err != nil || res.Requeue {
		return res, err
	}

	// Ensure Service
	if res, err := r.ensureService(ctx, instance, challenge); err != nil || res.Requeue {
		return res, err
	}

	// Ensure AttackBox deployment & service if enabled
	if res, err := r.ensureAttackBox(ctx, instance, challenge); err != nil || res.Requeue {
		return res, err
	}

	// Ensure Ingress
	if res, err := r.ensureIngress(ctx, instance, challenge); err != nil || res.Requeue {
		return res, err
	}

	// Ensure NetworkPolicy
	if res, err := r.ensureNetworkPolicy(ctx, instance, challenge); err != nil || res.Requeue {
		return res, err
	}

	// Check if Deployment is ready & update status
	if err := r.checkAndUpdateReady(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue to check status periodically
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// ensureDeployment creates/updates the primary Deployment for the instance
func (r *ChallengeInstanceReconciler) ensureDeployment(ctx context.Context, instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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
	return ctrl.Result{}, nil
}

// ensureService creates/updates the Service for the instance and updates connection info if needed
func (r *ChallengeInstanceReconciler) ensureService(ctx context.Context, instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	service := builder.BuildService(instance, challenge)
	if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference on Service")
		return ctrl.Result{}, err
	}

	existingService := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, existingService)
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
		// Service exists, update connection info if NodePort/LoadBalancer is assigned
		connInfo := builder.GetConnectionInfo(existingService, r.getNodeIP())
		if connInfo != "" && instance.Status.ConnectionInfo != connInfo {
			instance.Status.ConnectionInfo = connInfo
			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update connection info")
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

// ensureAttackBox creates attackbox deployment and service if configured
func (r *ChallengeInstanceReconciler) ensureAttackBox(ctx context.Context, instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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
		} else if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get AttackBox Deployment")
			return ctrl.Result{}, err
		}
	}

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
		} else if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get AttackBox Service")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ensureIngress creates ingress if configured and updates connection info
func (r *ChallengeInstanceReconciler) ensureIngress(ctx context.Context, instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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
		}

		// Always set connection info when Ingress is enabled (whether just created or already exists)
		// Only update if not already set to avoid overwriting
		if instance.Status.ConnectionInfo == "" {
			hostname := builder.GetIngressHostname(instance, challenge)
			if hostname != "" {
				if challenge.Spec.Scenario.AttackBox != nil && challenge.Spec.Scenario.AttackBox.Enabled {
					instance.Status.ConnectionInfo = fmt.Sprintf("Challenge: http://%s\nTerminal: http://%s/terminal", hostname, hostname)
				} else {
					instance.Status.ConnectionInfo = fmt.Sprintf("http://%s", hostname)
				}
				if err := r.Status().Update(ctx, instance); err != nil {
					log.Error(err, "Failed to update instance connection info after creating Ingress")
					return ctrl.Result{}, err
				}
				log.Info("Set connectionInfo for instance", "instance", instance.Name, "connectionInfo", instance.Status.ConnectionInfo)
				// Persist connectionInfo immediately
				if err := r.Status().Update(ctx, instance); err != nil {
					log.Error(err, "Failed to update instance status with connectionInfo")
				}
			}
		} else if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get Ingress")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// ensureNetworkPolicy creates networkpolicy if configured
func (r *ChallengeInstanceReconciler) ensureNetworkPolicy(ctx context.Context, instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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
		} else if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get NetworkPolicy")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// checkAndUpdateReady checks deployment readiness and updates instance status accordingly
func (r *ChallengeInstanceReconciler) checkAndUpdateReady(ctx context.Context, instance *ctfv1alpha1.ChallengeInstance) error {
	log := logf.FromContext(ctx)

	// If deployment name not set, nothing to do
	if instance.Status.DeploymentName == "" {
		return nil
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Status.DeploymentName, Namespace: instance.Namespace}, deployment); err != nil {
		return err
	}

	if deployment.Status.ReadyReplicas > 0 {
		if instance.Status.Phase != "Running" || !instance.Status.Ready {
			instance.Status.Phase = "Running"
			instance.Status.Ready = true

			// Update connection info from service if possible
			if instance.Status.ServiceName != "" {
				existingService := &corev1.Service{}
				if err := r.Get(ctx, types.NamespacedName{Name: instance.Status.ServiceName, Namespace: instance.Namespace}, existingService); err == nil {
					connInfo := builder.GetConnectionInfo(existingService, r.getNodeIP())
					if connInfo != "" {
						instance.Status.ConnectionInfo = connInfo
>>>>>>> 375a3d9 (fix: lint)
					}
				}
			}

			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update instance status to Running")
				return err
			}
			log.Info("Instance is now Running", "instance", instance.Name, "connectionInfo", instance.Status.ConnectionInfo)
		}
	}
	return nil
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