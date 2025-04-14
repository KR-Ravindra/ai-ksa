package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
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

	"fmt"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	autoscalingv1 "github.com/kr-ravindra/ai-ksa/api/v1"
	cron "github.com/robfig/cron/v3"
	batchv1 "k8s.io/api/batch/v1"
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
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=aihorizontalpodautoscalers/finalizers,verbs=update
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=scheduledscalers,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=scheduledscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.cortex.me,resources=scheduledscalers/finalizers,verbs=update

// AIHorizontalPodAutoscalerReconciler reconciles a AIHorizontalPodAutoscaler object
type AIHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile function
func (r *AIHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Reconciler call for AIHorizontalPodAutoscaler
	aihpa := &autoscalingv1.AIHorizontalPodAutoscaler{}
	err := r.Get(ctx, req.NamespacedName, aihpa)
	if err == nil {
		logger.V(1).Info("Reconciling AIHorizontalPodAutoscaler", "Name", req.Name, "Namespace", req.Namespace)
		return r.reconcileAIHorizontalPodAutoscaler(ctx, req)
	}

	// Reconciler call for ScheduledScaler
	scheduledScaler := &autoscalingv1.ScheduledScaler{}
	err = r.Get(ctx, req.NamespacedName, scheduledScaler)
	if err == nil {
		logger.V(1).Info("Reconciling ScheduledScaler", "Name", req.Name, "Namespace", req.Namespace)
		return r.reconcileScheduledScaler(ctx, scheduledScaler)
	}

	// Unknown reconcilation call
	if errors.IsNotFound(err) {
		logger.Info("Resource not found. Ignoring since object must be deleted", "Name", req.Name, "Namespace", req.Namespace)
		return ctrl.Result{}, nil
	}

	logger.Error(err, "Failed to get resource")
	return ctrl.Result{}, err
}

func sendNotification(message string, callBy string) error {
    webhookURL := os.Getenv("SLACK_WEBHOOK_URL")
    logger := log.FromContext(context.Background())
    logger.Info("Sending notification to Slack", "WebhookURL", webhookURL, "Message", message, "CallBy", callBy)
    if webhookURL == "" {
        webhookURL = "https://hooks.slack.com/services/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"
    }
    payload := map[string]interface{}{
        "blocks": []map[string]interface{}{
            {
                "type": "section",
                "text": map[string]interface{}{
                    "type": "mrkdwn",
                    "text": fmt.Sprintf(
                        "*ScheduledScaler Notification:*\n\n*Message:* %s\n*Call By:* `%s`",
                        message,
                        callBy,
                    ),
                },
            },
        },
    }
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return err
    }

    resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send notification, status code: %d", resp.StatusCode)
    }

    return nil
}

func (r *AIHorizontalPodAutoscalerReconciler) reconcileScheduledScaler(ctx context.Context, scheduledScaler *autoscalingv1.ScheduledScaler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling ScheduledScaler", "Name", scheduledScaler.Name, "Namespace", scheduledScaler.Namespace)
	cronJobName := fmt.Sprintf("%s-scheduler", scheduledScaler.Name)

	// Handle recurring scheduled scaling
	if scheduledScaler.Spec.Schedule != "" && !scheduledScaler.Spec.OneTime {
		logger.V(1).Info("Recurring ScheduledScaler detected", "Schedule", scheduledScaler.Spec.Schedule, "Duration", scheduledScaler.Spec.Duration)

		// Define the CronJob for scaling up
		scaleUpCronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-scale-up", cronJobName),
				Namespace: scheduledScaler.Namespace,
				Labels: map[string]string{
					"app":        "ai-ksa-scheduled-scaler",
					"scheduler":  scheduledScaler.Name,
					"controller": "aihorizontalpodautoscaler-controller",
				},
			},
			Spec: batchv1.CronJobSpec{
				Schedule: scheduledScaler.Spec.Schedule,
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:    "scaler",
										Image:   "curlimages/curl:latest",
										Command: []string{"/bin/sh", "-c"},
										Args: []string{
											fmt.Sprintf(`
                                                curl -X POST -H "Content-Type: application/json" -d '
                                                {
                                                    "namespace": "%s",
                                                    "deployment": "%s",
                                                    "metricType": "overwrite",
                                                    "instructReplicas": %d,
													"callBy": "scheduled-scaler"
                                                }
                                                ' http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger
                                            `, scheduledScaler.Namespace, scheduledScaler.Spec.TargetDeploymentName, scheduledScaler.Spec.Replicas),
										},
									},
								},
							},
						},
					},
				},
			},
		}

		// Define the CronJob for scaling down
		startCronExpression := scheduledScaler.Spec.Schedule
		duration := scheduledScaler.Spec.Duration
		// Calculate the end cron expression
		startTime, err := parseCronToNextTime(startCronExpression)
		if err != nil {
			logger.Error(err, "Failed to parse start cron expression", "Schedule", startCronExpression)
			return ctrl.Result{}, err
		}
		parsedDuration, err := time.ParseDuration(duration + "m")
		if err != nil {
			logger.Error(err, "Failed to parse duration", "Duration", duration)
			return ctrl.Result{}, err
		}
		endTime := startTime.Add(parsedDuration)
		logger.V(2).Info("Start time calculated", "StartTime", startTime)
		logger.V(2).Info("Duration parsed", "Duration", parsedDuration)
		logger.V(2).Info("End time calculated", "EndTime", endTime)
		endCronExpression := formatTimeToCron(endTime)

		scaleDownCronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-scale-down", cronJobName),
				Namespace: scheduledScaler.Namespace,
				Labels: map[string]string{
					"app":        "ai-ksa-scheduled-scaler",
					"scheduler":  scheduledScaler.Name,
					"controller": "aihorizontalpodautoscaler-controller",
				},
			},
			Spec: batchv1.CronJobSpec{
				Schedule: endCronExpression,
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:    "scaler",
										Image:   "curlimages/curl:latest",
										Command: []string{"/bin/sh", "-c"},
										Args: []string{
											fmt.Sprintf(`
                                                curl -X POST -H "Content-Type: application/json" -d '
                                                {
                                                    "namespace": "%s",
                                                    "deployment": "%s",
                                                    "metricType": "overwrite",
                                                    "instructReplicas": %d,
													"callBy": "scheduled-scaler"
                                                }
                                                ' http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger
                                            `, scheduledScaler.Namespace, scheduledScaler.Spec.TargetDeploymentName, scheduledScaler.Spec.EndReplicas),
										},
									},
								},
							},
						},
					},
				},
			},
		}

		// Create or update the scale-up CronJob
		if err := r.createOrUpdateCronJob(ctx, scaleUpCronJob, scheduledScaler); err != nil {
			logger.Error(err, "Failed to create or update scale-up CronJob")
			return ctrl.Result{}, err
		}

		// Create or update the scale-down CronJob
		if err := r.createOrUpdateCronJob(ctx, scaleDownCronJob, scheduledScaler); err != nil {
			logger.Error(err, "Failed to create or update scale-down CronJob")
			return ctrl.Result{}, err
		}

		logger.Info("Recurring CronJobs reconciled", "ScaleUpCronJob", scaleUpCronJob.Name, "ScaleDownCronJob", scaleDownCronJob.Name)

		message := fmt.Sprintf("ScheduledScaler %s created with schedule %s and duration %s", scheduledScaler.Name, scheduledScaler.Spec.Schedule, scheduledScaler.Spec.Duration)
		go sendNotification(message, "scheduled-scaler")

		return ctrl.Result{}, nil
	}

	// Handle one-time scheduled scaling
	if scheduledScaler.Spec.OneTime {
		logger.Info("One-time ScheduledScaler detected", "StartTime", scheduledScaler.Spec.StartTime)

		// Parse the start time
		startTime, err := time.Parse(time.RFC3339, scheduledScaler.Spec.StartTime)
		if err != nil {
			logger.Error(err, "Failed to parse start time", "StartTime", scheduledScaler.Spec.StartTime)
			return ctrl.Result{}, err
		}

		// Check if the start time is in the future
		currentTime := time.Now().UTC()
		if currentTime.Before(startTime) {
			// Requeue to check again closer to the start time
			requeueAfter := time.Until(startTime)
			logger.Info("Requeueing until start time", "RequeueAfter", requeueAfter)
			go sendNotification(fmt.Sprintf("ScheduledScaler %s will scale at %s", scheduledScaler.Name, startTime), "one-time-scaler")
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}

		// Trigger scaling logic
		logger.V(1).Info("Triggering one-time scaling logic", "Deployment", scheduledScaler.Spec.TargetDeploymentName, "Replicas", scheduledScaler.Spec.Replicas)
		// Fetch the target deployment
		targetDeployment := &appsv1.Deployment{}
		err = r.Get(ctx, types.NamespacedName{Name: scheduledScaler.Spec.TargetDeploymentName, Namespace: scheduledScaler.Namespace}, targetDeployment)
		if err != nil {
			logger.Error(err, "Failed to fetch target deployment", "Namespace", scheduledScaler.Namespace, "Name", scheduledScaler.Spec.TargetDeploymentName)
			return ctrl.Result{}, err
		}

		// Fetch the pods for the deployment
		podList := &corev1.PodList{}
		err = r.List(ctx, podList, client.InNamespace(scheduledScaler.Namespace), client.MatchingLabels(targetDeployment.Spec.Selector.MatchLabels))
		if err != nil {
			logger.Error(err, "Failed to list pods for target deployment", "Namespace", scheduledScaler.Namespace, "Name", scheduledScaler.Spec.TargetDeploymentName)
			return ctrl.Result{}, err
		}

		// Call scaleDeployment with the correct arguments
		logger.V(1).Info("Calling scaleDeployment for one-time scaling", "TargetDeployment", targetDeployment.Name, "Replicas", scheduledScaler.Spec.Replicas)
		err = r.scaleDeployment(ctx, logger, targetDeployment, podList, "overwrite", 0, &scheduledScaler.Spec.Replicas, "one-time-event")
		if err != nil {
			logger.Error(err, "Failed to trigger one-time scaling")
			return ctrl.Result{}, err
		}

		logger.Info("One-time scaling completed")
		return ctrl.Result{}, nil
	}

	logger.Info("No valid schedule or one-time event found for ScheduledScaler", "Name", scheduledScaler.Name)
	return ctrl.Result{}, nil
}

func parseCronToNextTime(cronExpression string) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpression)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(time.Now()), nil
}

func formatTimeToCron(t time.Time) string {
	return fmt.Sprintf("%d %d %d %d *", t.Minute(), t.Hour(), t.Day(), int(t.Month()))
}

func (r *AIHorizontalPodAutoscalerReconciler) createOrUpdateCronJob(ctx context.Context, cronJob *batchv1.CronJob, owner metav1.Object) error {
	existingCronJob := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: cronJob.Name, Namespace: cronJob.Namespace}, existingCronJob)
	if err != nil {
		if errors.IsNotFound(err) {
			// Set the owner reference
			if err := ctrl.SetControllerReference(owner, cronJob, r.Scheme); err != nil {
				return err
			}
			// Create the CronJob if it doesn't exist
			return r.Create(ctx, cronJob)
		}
		return err
	}

	// Update the CronJob if it exists but differs
	if !reflect.DeepEqual(existingCronJob.Spec, cronJob.Spec) {
		existingCronJob.Spec = cronJob.Spec
		return r.Update(ctx, existingCronJob)
	}

	return nil
}

func (r *AIHorizontalPodAutoscalerReconciler) reconcileAIHorizontalPodAutoscaler(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		err = r.scaleDeployment(ctx, logger, targetDeployment, podList, "cpu", int64(*aihpa.Spec.TargetDeploymentCPUThreshold), nil, "controller-cpu")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// 5. Scale based on Memory (if the field is provided)
	if aihpa.Spec.TargetDeploymentMemoryThreshold != nil {
		err = r.scaleDeployment(ctx, logger, targetDeployment, podList, "memory", int64(*aihpa.Spec.TargetDeploymentMemoryThreshold), nil, "controller-memory")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// 6. Requeue periodically to monitor metrics
	return ctrl.Result{RequeueAfter: time.Second * 15}, nil
}

func (r *AIHorizontalPodAutoscalerReconciler) scaleDeployment(ctx context.Context, logger logr.Logger, targetDeployment *appsv1.Deployment, podList *corev1.PodList, metricType string, threshold int64, instructReplicas *int32, callBy string) error {
	totalUsage := int64(0)
	logger.V(1).Info("Scaling deployment called with arguments", "MetricType", metricType, "Threshold", threshold, "InstructReplicas", instructReplicas)
	if metricType == "overwrite" {
		if instructReplicas != nil && *instructReplicas > 0 {
			logger.Info("InstructReplicas provided, updating deployment", "Deployment", targetDeployment, "InstructReplicas", *instructReplicas)
			targetDeployment.Spec.Replicas = instructReplicas
			err := r.Update(ctx, targetDeployment)
			if err != nil {
				logger.Error(err, "Failed to update deployment replicas")
				return err
			}
			logger.Info("Scaled deployment based on instruct replicas ", "InstructReplicas", *instructReplicas, "CallBy", callBy)
			go sendNotification(fmt.Sprintf("Scaled deployment %s/%s to %d replicas based on instruct replicas as a call by %s", targetDeployment.Namespace, targetDeployment.Name, *instructReplicas, callBy), callBy)
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
	var newReplicas int32

	if averageUsage > threshold {
		newReplicas = currentReplicas + 1
	} else if averageUsage < threshold && currentReplicas > 1 {
		newReplicas = currentReplicas - 1
	} else {
		// No scaling needed
		return nil
	}
	targetDeployment.Spec.Replicas = &newReplicas
	err := r.Update(ctx, targetDeployment)
	if err != nil {
		logger.Error(err, "Failed to update deployment replicas")
		return err
	}
	logger.Info("Scaled deployment", "NewReplicas", newReplicas)

	message := fmt.Sprintf("Scaled deployment %s/%s from %d to %d based on %s usage", targetDeployment.Namespace, targetDeployment.Name, currentReplicas, newReplicas, metricType)
	go sendNotification(message, callBy)
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
		MetricType       string `json:"metricType"` // e.g., "cpu" or "memory"
		Threshold        int64  `json:"threshold,omitempty"`
		InstructReplicas int32  `json:"instructReplicas,omitempty"`
		CallBy           string `json:"callBy,omitempty"`
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
	err = r.scaleDeployment(ctx, logger, targetDeployment, podList, payload.MetricType, payload.Threshold, instructReplicas, payload.CallBy)
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
		Watches(&autoscalingv1.ScheduledScaler{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
