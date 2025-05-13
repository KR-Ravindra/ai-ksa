# AI-KSA

AI-KSA is focused on building an AI enabled Kubernetes Scaling Agent to help with Horizantal Pod Autoscaling. 

## Architecture

![](./documentation/aiksa.png)


## Comparision 

A comparision table is given below, so as to compare with existing best performing autoscalers solving horizantal pod autoscaling in kubernetes.

| Horizantal Pod Autoscaler | Scaling by CPU, RAM usage | CPU, RAM usage by real values | External Events | Metrics | AI enabled | Webhook | Schedule scaling | Custom Enhancements | Easy of Use |
| ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ---------- | ---------- |
| KEDA ![](./documentation/keda-logo.png) | :white_check_mark: | :x: | :white_check_mark: (limited) | :white_check_mark: | :x: | :x: | :white_check_mark: | :white_check_mark: (limited) | Medium |
| Kubernetes HPA ![](./documentation/hpa.jpg) | :white_check_mark: | :x: | :x: | :white_check_mark: (beta, needs translation) | :x: | :x: | :x: |  :x: | Very Easy |
| AI-KSA | :x:| :white_check_mark: | :white_check_mark: (Need to extend) | :white_check_mark: | :white_check_mark: (4 AI techniques) | :white_check_mark: | :white_check_mark: | :white_check_mark: | Very Easy |


## Highlights

- [x] Option to abstract metrics logic out of autoscaler and even Kuberentes itself, for overriding base scaling logic of Kubernetes - `desiredReplicas = ceil[currentReplicas * ( currentMetricValue / desiredMetricValue )]`

- [x] Scaling based on CPU, RAM real values as Horizantal Pod Scaling != Unlimited Node Scaling

- [x] Agentic approach with AI based decisions and predictions

- [x] Can be triggered by webhook, labels and by CRDs

- [x] Alertmanager - alerts to scaling via webhook

- [x] External events for scaling via webhook

- [x] Scheduled scaling for recurring events with auto scale down via Kubernetes CronJobs

- [x] Scheduling scaling capabilities for non-recurring events

- [x] On the fly AI based decisions with support for techniques - decision trees, arima, gradient_boosting, rule_based

- [x] Ability to move scaling agent out of target Kubernetes Cluster.


## CRDs 

- AIHorizontalPodAutoscaler.autoscaling.cortex.me

```yaml
apiVersion: autoscaling.cortex.me/v1 
kind: AIHorizontalPodAutoscaler
metadata:
  name: my-unique-autoscaler 
  namespace: default
spec:
  targetDeploymentName: my-deployment 
  targetDeploymentNamespace: my-namespace 
  targetDeploymentCPUThreshold: 60 

```

- ScheduledScaler.autoscaling.cortex.me

```yaml
apiVersion: autoscaling.cortex.me/v1
kind: ScheduledScaler
metadata:
  name: my-recurring-scaler
  namespace: default
spec:
  targetDeploymentName: my-deployment
  targetDeploymentNamespace: my-namespace
  schedule: "*/1 * * * *" # Every 5 minutes
  duration: "10"            # Duration in minutes
  replicas: 20            # Number of replicas to scale up to
  endReplicas: 10         # Number of replicas to scale down to after the duration
  oneTime: false         # Indicates this is a recurring scaling event
```

### Scaling by labels + AI

To make use of Agentic AI scaling; Your deployments need to be labeled `ai-scaling=true`. For example consider the following command

```
kubernetes label deployment default/ai-based ai-scaling=true
```

You can find this simulation at [here](./test/simulation.sh) and in the video documentation [on youtube](https://youtu.be/05cD0QC6i4U)

## Operator Manifests
List of manifests deployed as part of the operator.
```
namespace/operator-system 
customresourcedefinition.apiextensions.k8s.io/aihorizontalpodautoscalers.autoscaling.cortex.me 
customresourcedefinition.apiextensions.k8s.io/scheduledscalers.autoscaling.cortex.me 
serviceaccount/operator-controller-manager 
role.rbac.authorization.k8s.io/operator-leader-election-role 
clusterrole.rbac.authorization.k8s.io/operator-aihorizontalpodautoscaler-editor-role 
clusterrole.rbac.authorization.k8s.io/operator-aihorizontalpodautoscaler-viewer-role 
clusterrole.rbac.authorization.k8s.io/operator-manager-role 
clusterrole.rbac.authorization.k8s.io/operator-metrics-auth-role 
clusterrole.rbac.authorization.k8s.io/operator-metrics-reader 
clusterrole.rbac.authorization.k8s.io/operator-scheduledscaler-editor-role 
clusterrole.rbac.authorization.k8s.io/operator-scheduledscaler-viewer-role 
rolebinding.rbac.authorization.k8s.io/operator-leader-election-rolebinding 
clusterrolebinding.rbac.authorization.k8s.io/operator-manager-rolebinding 
clusterrolebinding.rbac.authorization.k8s.io/operator-metrics-auth-rolebinding 
service/operator-controller-manager-autoscale-trigger 
service/operator-controller-manager-metrics-service 
deployment.apps/operator-controller-manager
deployment.apps/operator-ai-scaling-agent
```


## To Install on cluster from releases

Follow instructions for your release [here](https://github.com/KR-Ravindra/ai-ksa/releases)
Agent configuration could be tweaked by using environment variables on `deployment.apps/operator-ai-scaling-agent`

List of environment variables:

| Environment Variable | Description | Default Value | Kubernetes Resource |
| ----------- | ----------- | ----------- | ------- |
| CPU_HISTORY_LENGTH | Historical Records to store in regards of CPU & Memory before generating scaling decisions | 10 | `deployment.apps/operator-ai-scaling-agent` |
| SCAN_INTERVAL | Historical Records to store in regards of RAM before generating scaling decisions | 15 (in seconds) | `deployment.apps/operator-ai-scaling-agent` |
| SCALE_UP_THRESHOLD | Scale up threshold in real values for CPU | 800 (in nm) | `deployment.apps/operator-ai-scaling-agent` |
| SCALE_DOWN_THRESHOLD | Scale down threshold in real values for CPU | 200 (in nm) | `deployment.apps/operator-ai-scaling-agent` |
| MODEL_TYPE | Select the AI technique used by the agent. Available options: arima (Autoregressive integrated moving average), decision_tree, gradient_boosting, rule_based | arima | `deployment.apps/operator-ai-scaling-agent` |
| CONSISTENCY_THRESHOLD | Measure of consistent prediction before actual scaling | 2 | `deployment.apps/operator-ai-scaling-agent` |
| AUTOSCALER_API_URL | API URL used by agent to call scaling controller for scaling decisions | `controller-manager-autoscale-trigger` (assuming agent runs within cluster) | `deployment.apps/operator-ai-scaling-agent` |
| NOTIFICATION_URL | API URL used by controller for sending in notifications to selected channel | `https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXX` (fetches from secret, make sure you have this secret available) | `deployment.apps/operator-controller-manager` |


## Webhook usage for third party integration

Within Cluster
```
curl -X POST http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "external",
  "metricType": "overwrite",
  "instructReplicas": 20
}'
```
Outside Cluster
```
 curl -X POST http://x.y.com/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "ai-based",
  "metricType": "overwrite",
  "instructReplicas": 20,
  "callBy": "demo-trigger"
}'
```
```
curl -X POST http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "somegix",
  "metricType": "overwrite",
  "instructReplicas": 20,
  "callBy": "sample"
}'
```
## Video documentation on [YOUTUBE HERE!](https://youtu.be/05cD0QC6i4U)
