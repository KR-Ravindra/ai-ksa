package controller

import (
	"context"
	"encoding/json"
	"net/http"
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

	"github.com/go-logr/logr"
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
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods,verbs=get;list;watch
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

	// 4. Scale based on CPU
	if aihpa.Spec.TargetDeploymentCPUThreshold != nil {
		err = r.scaleDeployment(ctx, logger, targetDeployment, podList, "cpu", int64(*aihpa.Spec.TargetDeploymentCPUThreshold), int32(aihpa.Spec.MinReplicas), int32(aihpa.Spec.MaxReplicas), nil)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// 5. Scale based on Memory (if the field is provided)
	if aihpa.Spec.TargetDeploymentMemoryThreshold != nil {
		err = r.scaleDeployment(ctx, logger, targetDeployment, podList, "memory", int64(*aihpa.Spec.TargetDeploymentMemoryThreshold), int32(aihpa.Spec.MinReplicas), int32(aihpa.Spec.MaxReplicas), nil)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// 6. Requeue periodically to monitor metrics
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func (r *AIHorizontalPodAutoscalerReconciler) scaleDeployment(ctx context.Context, logger logr.Logger, targetDeployment *appsv1.Deployment, podList *corev1.PodList, metricType string, threshold int64, minReplicas int32, maxReplicas int32, instructReplicas *int32) error {
	totalUsage := int64(0)
	if metricType == "overwrite" {
		if instructReplicas != nil && *instructReplicas > 0 {
			logger.Info("InstructReplicas provided, updating deployment", targetDeployment, "InstructReplicas", *instructReplicas)
			targetDeployment.Spec.Replicas = instructReplicas
			err := r.Update(ctx, targetDeployment)
			if err != nil {
				logger.Error(err, "Failed to update deployment replicas")
				return err
			}
			logger.Info("Deployment replicas updated successfully", targetDeployment, "NewReplicas", *instructReplicas)
			return nil
		}
	}
	// Query metrics for each pod
	for _, pod := range podList.Items {
		metrics, err := metricsClientSet.MetricsV1beta1().PodMetricses(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			logger.Error(err, "Failed to get metrics for pod", "Pod.Name", pod.Name)
			continue
		}

		// Accumulate usage based on metric type
		for _, container := range metrics.Containers {
			var usage int64
			if metricType == "cpu" {
				usage = container.Usage.Cpu().MilliValue() // CPU in milliCPU
			} else if metricType == "memory" {
				usage = container.Usage.Memory().Value() // Memory in bytes
			}
			totalUsage += usage
		}
	}

	// Calculate average usage
	averageUsage := totalUsage / int64(len(podList.Items))
	logger.V(1).Info("Metric Usage", "MetricType", metricType, "AverageUsage", averageUsage, "Threshold", threshold)

	// Scale logic
	currentReplicas := *targetDeployment.Spec.Replicas
	if averageUsage > threshold && currentReplicas < maxReplicas {
		newReplicas := currentReplicas + 1
		targetDeployment.Spec.Replicas = &newReplicas
		err := r.Update(ctx, targetDeployment)
		if err != nil {
			logger.Error(err, "Failed to scale up target deployment")
			return err
		}
		logger.Info("Scaled up target deployment", "NewReplicas", newReplicas)
	} else if averageUsage < threshold && currentReplicas > minReplicas {
		newReplicas := currentReplicas - 1
		targetDeployment.Spec.Replicas = &newReplicas
		err := r.Update(ctx, targetDeployment)
		if err != nil {
			logger.Error(err, "Failed to scale down target deployment")
			return err
		}
		logger.Info("Scaled down target deployment", "NewReplicas", newReplicas)
	}

	return nil
}

func (r *AIHorizontalPodAutoscalerReconciler) handleWebhook(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request body
	var payload struct {
		Namespace        string `json:"namespace"`
		Deployment       string `json:"deployment"`
		MetricType       string `json:"metricType` // e.g., "cpu" or "memory"
		Threshold        int64  `json:"threshold,omitempty"`
		MinReplicas      int32  `json:"minReplicas,omitempty"`
		MaxReplicas      int32  `json:"maxReplicas,omitempty"`
		InstructReplicas int32  `json:"instructReplicas,omitempty"`
	}
	err := json.NewDecoder(req.Body).Decode(&payload)
	if err != nil {
		http.Error(w, "Failed to parse request body", http.StatusBadRequest)
		return
	}

	if payload.MetricType == "" {
        payload.MetricType = "cpu" // Default to "cpu"
    }
    if payload.Threshold == 0 {
        payload.Threshold = 80 // Default threshold
    }
    if payload.MinReplicas == 0 {
        payload.MinReplicas = 1 // Default minimum replicas
    }
    if payload.MaxReplicas == 0 {
        payload.MaxReplicas = 10 // Default maximum replicas
    }

	// Fetch the target deployment
	ctx := context.Background()
	logger := log.FromContext(ctx)
	targetDeployment := &appsv1.Deployment{}
	logger.Info("Received webhook payload", "Payload", payload)
	err = r.Get(ctx, types.NamespacedName{Name: payload.Deployment, Namespace: payload.Namespace}, targetDeployment)
	if err != nil {
		http.Error(w, "Failed to fetch target deployment: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the pods for the deployment
	podList := &corev1.PodList{}
	err = r.List(ctx, podList, client.InNamespace(payload.Namespace), client.MatchingLabels(targetDeployment.Spec.Selector.MatchLabels))
	if err != nil {
		http.Error(w, "Failed to list pods: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var instructReplicas *int32
	if payload.MetricType == "overwrite" {
		if payload.InstructReplicas > 0 {
			instructReplicas = &payload.InstructReplicas
		}
	}
	// Trigger scaling logic
	err = r.scaleDeployment(ctx, logger, targetDeployment, podList, payload.MetricType, payload.Threshold, payload.MinReplicas, payload.MaxReplicas, instructReplicas)
	if err != nil {
		http.Error(w, "Failed to scale deployment: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Respond with success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Scaling decision triggered successfully"))
}

func (r *AIHorizontalPodAutoscalerReconciler) startWebhookServer() {
	http.HandleFunc("/trigger", r.handleWebhook)
	port := "8080" // Define the port for the webhook server
	log := log.FromContext(context.Background())
	log.Info("Starting webhook server", "port", port)

	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Error(err, "Failed to start webhook server")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *AIHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {

	go r.startWebhookServer()

	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.AIHorizontalPodAutoscaler{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
