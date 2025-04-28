#!/bin/bash

export KUBECONFIG=~/kubeconfigs/config

# cleanup 
if [ "$1" == "cleanup" ]; then
  echo "Cleaning up..."
  kubectl delete deployment cpu
  kubectl delete deployment memory
  kubectl delete deployment external
  kubectl delete deployment ai-based
  kubectl delete deployment onetime-scaler
  kubectl delete deployment recurring-scaler
  kubectl delete -f ./cpu-crd.yaml
  kubectl delete -f ./memory-crd.yaml
  kubectl delete -f ./scheduler-onetime.yaml
  kubectl delete -f ./scheduler-recurring.yaml
  exit 0
    echo "Done!"
fi


# Create deployments with name cpu, memory, external, ai-based, onetime-scaler, recurring-scaler

echo "Creating deployments..."
kubectl create deployment cpu --image=nginx --replicas=1
kubectl create deployment memory --image=nginx --replicas=1
kubectl create deployment external --image=nginx --replicas=1
kubectl create deployment ai-based --image=nginx --replicas=1
kubectl create deployment onetime-scaler --image=nginx --replicas=1
kubectl create deployment recurring-scaler --image=nginx --replicas=1

# Deploy cpu-autoscaler and memory-autoscaler

echo "Deploying all the different autoscalers..."
kubectl apply -f ./cpu-crd.yaml
kubectl apply -f ./memory-crd.yaml

# Get time in RFC3339 format
TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
# Replace TIME_GOES_HERE in scheduler-onetime.yaml with the current time
sed -i 's/TIME_GOES_HERE/$TIME/g' ./scheduler-onetime.yaml

kubectl apply -f ./scheduler-onetime.yaml
kubectl apply -f ./scheduler-recurring.yaml

# Wait for all deployments to be up
echo "Waiting for all deployments to be up..."
kubectl wait --for=condition=available --timeout=20s deployment/cpu
kubectl wait --for=condition=available --timeout=20s deployment/memory
kubectl wait --for=condition=available --timeout=20s deployment/external
kubectl wait --for=condition=available --timeout=20s deployment/ai-based
kubectl wait --for=condition=available --timeout=20s deployment/onetime-scaler
kubectl wait --for=condition=available --timeout=20s deployment/recurring-scaler

# Wait for all pods to be running
echo "Waiting for all pods to be running..."
kubectl wait --for=condition=ready --timeout=20s pod -l app=cpu
kubectl wait --for=condition=ready --timeout=20s pod -l app=memory
kubectl wait --for=condition=ready --timeout=20s pod -l app=external
kubectl wait --for=condition=ready --timeout=20s pod -l app=ai-based
kubectl wait --for=condition=ready --timeout=20s pod -l app=onetime-scaler
kubectl wait --for=condition=ready --timeout=20s pod -l app=recurring-scaler

# Generate load on CPU and memory deployments
echo "Generating load on CPU and memory deployments..."
kubectl exec -it $(kubectl get pods -l app=cpu -o jsonpath='{.items[0].metadata.name}') -- yes > /dev/null &
kubectl exec -it $(kubectl get pods -l app=memory -o jsonpath='{.items[0].metadata.name}') -- yes > /dev/null &

# Scaling via external curl
echo "Scaling via external curl..."
kubectl exec -it $(kubectl get pods -l app=external -o jsonpath='{.items[0].metadata.name}') -- curl -X POST http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger -H "Content-Type: application/json" -d '{
  "namespace": "default",
  "deployment": "external",
  "metricType": "overwrite",
  "instructReplicas": 20,
  "callBy": "demo-trigger"
}'

# Enable scaling via AI-based
echo "Enabling scaling via AI-based..."
echo "Adding label to ai-based deployment..."
kubectl label deployment ai-based autoscale=true
echo "Label added"
kubectl get deployment ai-based --show-labels

echo "Await AI based scaling..."
echo "Get logs of agent operator-ai-scaling-agent..."
kubectl logs deployment/agent-operator-ai-scaling-agent -n operator-system --tail=1000 

