package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1 "github.com/kr-ravindra/ai-ksa/api/v1"
)

var (
	k8sClientSet     *kubernetes.Clientset
	metricsClientSet *metricsv.Clientset
)

// Initialize Kubernetes and Metrics clients
func init() {
	logger := log.FromContext(context.Background())
	config, err := rest.InClusterConfig() // Works when running inside the cluster
	if err != nil {
		logger.Error(err, "Failed to get in-cluster config, trying to load kubeconfig")
		if err != nil {
			panic(err)
		}
	}
	k8sClientSet, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	metricsClientSet, err = metricsv.NewForConfig(config)
	if err != nil {
		panic(err)
	}
}
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers/finalizers,verbs=update

// AIHorizontalPodAutoscalerReconciler reconciles a AIHorizontalPodAutoscaler object
type AIHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile function
func (r *AIHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the AIHorizontalPodAutoscaler instance
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

	// 2. Fetch the target deployment
	targetDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: aihpa.Spec.TargetDeploymentName, Namespace: aihpa.Spec.TargetDeploymentNamespace}, targetDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "Target deployment not found", "Namespace", aihpa.Spec.TargetDeploymentNamespace, "Name", aihpa.Spec.TargetDeploymentName)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get target deployment")
		return ctrl.Result{}, err
	}

	// 3. Query the Metrics Server for CPU usage
	podList := &corev1.PodList{}
	err = r.List(ctx, podList, client.InNamespace(aihpa.Spec.TargetDeploymentNamespace), client.MatchingLabels(targetDeployment.Spec.Selector.MatchLabels))
	if err != nil {
		logger.Error(err, "Failed to list pods for target deployment")
		return ctrl.Result{}, err
	}

	totalCPUUsage := int64(0)
	for _, pod := range podList.Items {
		metrics, err := metricsClientSet.MetricsV1beta1().PodMetricses(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			logger.Error(err, "Failed to get metrics for pod", "Pod.Name", pod.Name)
			continue
		}
		for _, container := range metrics.Containers {
			cpuUsage, _ := container.Usage.Cpu().AsInt64()
			totalCPUUsage += cpuUsage
		}
	}

	// Calculate average CPU usage
	averageCPUUsage := totalCPUUsage / int64(len(podList.Items))
	logger.Info("CPU Usage", "AverageCPUUsage", averageCPUUsage, "Threshold", aihpa.Spec.TargetDeploymentCPUThreshold)

	// 4. Scale the deployment if CPU usage exceeds the threshold
	currentReplicas := *targetDeployment.Spec.Replicas
	if averageCPUUsage > int64(aihpa.Spec.TargetDeploymentCPUThreshold) && currentReplicas < int32(aihpa.Spec.MaxReplicas) {
		newReplicas := currentReplicas + 1
		targetDeployment.Spec.Replicas = &newReplicas
		err = r.Update(ctx, targetDeployment)
		if err != nil {
			logger.Error(err, "Failed to scale up target deployment")
			return ctrl.Result{}, err
		}
		logger.Info("Scaled up target deployment", "NewReplicas", newReplicas)
	} else if averageCPUUsage < int64(aihpa.Spec.TargetDeploymentCPUThreshold) && currentReplicas > int32(aihpa.Spec.MinReplicas) {
		newReplicas := currentReplicas - 1
		targetDeployment.Spec.Replicas = &newReplicas
		err = r.Update(ctx, targetDeployment)
		if err != nil {
			logger.Error(err, "Failed to scale down target deployment")
			return ctrl.Result{}, err
		}
		logger.Info("Scaled down target deployment", "NewReplicas", newReplicas)
	}

	// 5. Requeue periodically to monitor CPU usage
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AIHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.AIHorizontalPodAutoscaler{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
