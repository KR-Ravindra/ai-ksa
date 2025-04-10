# ai-ksa

| Horizantal Pod Autoscaler | Scaling by CPU, RAM usage | CPU, RAM usage against Node | Events | Metrics | AI enabled | Webhook | Custom Enhancements | Easy of Use |
| ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | ---------- |
| KEDA ![](./documentation/keda-logo.png) | :white_check_mark: | :x: | :white_check_mark: (limited) | :x: | :x: | :x: | :white_check_mark: (limited) | Medium |
| Kubernets HPA ![](./documentation/hpa.jpg) | :white_check_mark: | :x: | :x: | :white_check_mark: (beta, limited) | :x: | :x: | :x: | Very Easy |
| AI-KSA | :white_check_mark: | :white_check_mark: | :white_check_mark: (Need to extend) | :white_check_mark: | :white_check_mark: (4 AI models) | :white_check_mark: | :white_check_mark: | Very Easy |

















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
curl -X POST http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "somegix",
  "metricType": "overwrite",
  "instrcutReplicas": 20
}'
