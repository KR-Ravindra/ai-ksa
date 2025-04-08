/*
Copyright 2025.

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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1 "github.com/kr-ravindra/ai-ksa/api/v1"
)

// AIHorizontalPodAutoscalerReconciler reconciles a AIHorizontalPodAutoscaler object
type AIHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AIHorizontalPodAutoscaler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile

var k8sClientSet *kubernetes.Clientset

func init() {
    config, err := rest.InClusterConfig() // Works when running inside the cluster
    if err != nil {
        kubeconfigPath := "/path/to/your/kubeconfig" // Replace with your kubeconfig path
        config, err = config.BuildConfigFromFlags("", kubeconfigPath) // Works for local testing
        if err != nil {
            panic(err)
        }
    }
    k8sClientSet, err = kubernetes.NewForConfig(config)
    if err != nil {
        panic(err)
    }
}


func (r *AIHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	/// 1. Fetch the AIHorizontalPodAutoscaler instance
	aihpa := &autoscalingv1.AIHorizontalPodAutoscaler{}
	err := r.Get(ctx, req.NamespacedName, aihpa)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("AIHorizontalPodAutoscaler resource not found. Ignoring since object must be absent")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get AIHorizontalPodAutoscaler")
		return ctrl.Result{}, err
	}

	// 2. Define the "hello world" Deployment
	logger.Info("Reconciling AIHorizontalPodAutoscaler", "AIHorizontalPodAutoscaler.Namespace", aihpa.Namespace, "AIHorizontalPodAutoscaler.Name", aihpa.Name)
	helloWorldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hello-world-" + aihpa.Name,
			Namespace: aihpa.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "hello-world-" + aihpa.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "hello-world-" + aihpa.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "hello-world",
							Image:           "busybox", // Use a simple image
							Command:         []string{"/bin/sh", "-c", "echo 'Hello from my Operator!' && sleep 3600"},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			},
		},
	}

	// Set the AIHorizontalPodAutoscaler instance as the owner and controller of the Deployment
	ctrl.SetControllerReference(aihpa, helloWorldDeployment, r.Scheme)

	// 3. Check if the Deployment exists and create it if it doesn't
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: helloWorldDeployment.Name, Namespace: helloWorldDeployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new Deployment", "Deployment.Namespace", helloWorldDeployment.Namespace, "Deployment.Name", helloWorldDeployment.Name)
		err = r.Create(ctx, helloWorldDeployment)
		if err != nil {
			logger.Error(err, "Failed to create Deployment", "Deployment.Namespace", helloWorldDeployment.Namespace, "Deployment.Name", helloWorldDeployment.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil // Requeue to ensure status is updated
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment", "Deployment.Namespace", helloWorldDeployment.Namespace, "Deployment.Name", helloWorldDeployment.Name)
		return ctrl.Result{}, err
	}

	// 4. (We're NOT doing any scaling logic in this simplified version)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AIHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.AIHorizontalPodAutoscaler{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
