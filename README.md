# AI-KSA

AI-KSA is focused on building an AI enabled Kubernetes Scaling Agent to help with Horizantal Pod Autoscaling. 

## Architecture

![](./documentation/aiksa.png)

## Highlights

- [x] Option to abstract metrics logic out of autoscaler for overriding base scaling logic of Kubernetes - `desiredReplicas = ceil[currentReplicas * ( currentMetricValue / desiredMetricValue )]`

- [x] Scaling based on CPU, RAM real values as Horizantal Pod Scaling != Unlimited Node Scaling

- [x] Agentic approach with AI based decisions and predictions

- [x] Can be triggered by webhook, labels and by CRDs

- [x] Alertmanager - alerts to scaling via webhook

- [x] External events for scaling via webhook

- [x] Scheduled scaling  capabilities for recurring events with auto scale down events via Kubernetes native cron jobs

- [x] Scheduling scaling capabilities for non-recurring events

- [x] On the fly AI based decisions with support for techniques - decision trees, arima, gradient_boosting, rule_based

- [ ] Adjustable reconcilation time 

- [ ] AI Agent with a progressive knowledge base for identifying long term patterns

:interrobang: Integration with MCP Chat based AI Engine for human + AI decision making for pod autoscaling

## Comparision 

A comparision table is given below, so as to compare with existing best performing autoscalers solving horizantal pod autoscaling in kubernetes.

| Horizantal Pod Autoscaler | Scaling by CPU, RAM usage | CPU, RAM usage against Node | External Events | Metrics | AI enabled | Webhook | Schedule scaling | Custom Enhancements | Easy of Use |
| ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ---------- | ---------- |
| KEDA ![](./documentation/keda-logo.png) | :white_check_mark: | :x: | :white_check_mark: (limited) | :white_check_mark: | :x: | :x: | :white_check_mark: | :white_check_mark: (limited) | Medium |
| Kubernetes HPA ![](./documentation/hpa.jpg) | :white_check_mark: | :x: | :x: | :white_check_mark: (beta, needs translation) | :x: | :x: | :x: |  :x: | Very Easy |
| AI-KSA | :white_check_mark: | :white_check_mark: | :white_check_mark: (Need to extend) | :white_check_mark: | :white_check_mark: (4 AI techniques) | :white_check_mark: | :white_check_mark: | :white_check_mark: | Very Easy |

## Using Webhook

Within Cluster
```
curl -X POST http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "somegix",
  "metricType": "cpu",
  "threshold": 1000,
  "minReplicas": 1,
  "maxReplicas": 10
}'
```
Outside Cluster
```
curl -X POST http://134.199.185.13:32522/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "somegix",
  "metricType": "cpu",
  "threshold": 0,
  "minReplicas": 1,
  "maxReplicas": 10
}'
```
```
curl -X POST http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "somegix",
  "metricType": "overwrite",
  "instrcutReplicas": 20
}'
```