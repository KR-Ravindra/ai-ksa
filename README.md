# ai-ksa

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
