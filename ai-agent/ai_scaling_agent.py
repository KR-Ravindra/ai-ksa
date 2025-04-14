import os
import time
from kubernetes import client, config
from kubernetes.client.rest import ApiException
import numpy as np
import pandas as pd
from statsmodels.tsa.arima.model import ARIMA
from sklearn.tree import DecisionTreeRegressor
from sklearn.ensemble import GradientBoostingRegressor
from collections import defaultdict
from typing import List, Dict, Tuple
import requests
import logging

# Configuration
CPU_HISTORY_LENGTH = os.environ.get("CPU_HISTORY_LENGTH", 10)  # Number of past CPU metrics to consider
SCAN_INTERVAL = os.environ.get("SCAN_INTERVAL", 15)  # Time in seconds between scans
AUTOSCALER_API_URL = os.environ.get("AUTOSCALER_API_URL", "http://operator-controller-manager-autoscale-trigger.operator-system.svc.cluster.local:8080/trigger")
SCALE_UP_THRESHOLD = os.environ.get("SCALE_UP_THRESHOLD", 800)  # CPU percentage to trigger scale-up
SCALE_DOWN_THRESHOLD = os.environ.get("SCALE_DOWN_THRESHOLD", 200)  # CPU percentage to trigger scale-down
MODEL_TYPE = os.environ.get("MODEL_TYPE", "arima")  # Model type: decision_tree, gradient_boosting, arima (Autoregressive integrated moving average), rule_based
CONSISTENCY_THRESHOLD = os.environ.get("CONSISTENCY_THRESHOLD", 3)  # Number of recent decisions to check for consistency

# Global Variables
cpu_history: Dict[str, List[int]] = defaultdict(list)
ram_history: Dict[str, List[int]] = defaultdict(list)
models: Dict[str, any] = {}
model_metadata: Dict[str, Dict[str, any]] = {}
scaling_decision_history: Dict[str, List[int]] = defaultdict(list)  # Tracks recent scaling decisions for each deployment

# Logging setup
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

def main():
    logger.info("Starting AI Scaling Agent...")
    logger.info("Starting AI agent with configuration parameters:")
    logger.info(f"CPU_HISTORY_LENGTH: {CPU_HISTORY_LENGTH}")
    logger.info(f"SCAN_INTERVAL: {SCAN_INTERVAL}")
    logger.info(f"SCALE_UP_THRESHOLD: {SCALE_UP_THRESHOLD}")
    logger.info(f"SCALE_DOWN_THRESHOLD: {SCALE_DOWN_THRESHOLD}")
    logger.info(f"MODEL_TYPE: {MODEL_TYPE}")
    logger.info(f"CONSISTENCY_THRESHOLD: {CONSISTENCY_THRESHOLD}")
    logger.info("Initializing Kubernetes client...")
    logger.info(f"Using model type: {MODEL_TYPE}")

    # Kubernetes Client Setup
    try:
        config.load_incluster_config()
        logger.info("Loaded in-cluster Kubernetes configuration.")
    except config.ConfigException:
        config.load_kube_config()
        logger.info("Loaded local Kubernetes configuration.")
    core_v1 = client.CoreV1Api()
    apps_v1 = client.AppsV1Api()
    custom_objects_api = client.CustomObjectsApi()  # Use CustomObjectsApi for metrics

    while True:
        try:
            logger.info("Scanning deployments...")
            # Discover Deployments
            deployments = apps_v1.list_deployment_for_all_namespaces(label_selector="ai-scaling=true").items
            for deployment in deployments:
                namespace = deployment.metadata.namespace
                deployment_name = deployment.metadata.name
                full_name = f"{namespace}/{deployment_name}"

                # Skip system namespaces
                if namespace == "kube-system":
                    logger.debug(f"Skipping system namespace for deployment: {full_name}")
                    continue

                # Get CPU and RAM Metrics
                try:
                    logger.debug(f"Fetching CPU and RAM metrics for deployment: {full_name}")
                    cpu_usage, ram_usage = get_metrics(custom_objects_api, deployment_name, namespace, deployment.spec.selector.match_labels)
                    logger.info(f"Metrics for {full_name}: CPU={cpu_usage}, RAM={ram_usage}")
                except Exception as e:
                    logger.error(f"Error getting metrics for {full_name}: {e}")
                    continue

                # Update History
                logger.debug(f"Updating history for deployment: {full_name}")
                update_history(full_name, cpu_usage, ram_usage)

                # Initialize Model if Needed
                if full_name not in models:
                    logger.info(f"Initializing model for deployment: {full_name}")
                    initialize_model(full_name)

                # Get the current number of replicas
                current_replicas = deployment.spec.replicas

                # Make Scaling Decision
                logger.debug(f"Making scaling decision for deployment: {full_name}")
                desired_replicas, feature_importance = make_scaling_decision(full_name, current_replicas)

                # Update Scaling Decision History
                update_scaling_decision_history(full_name, desired_replicas)

                # Check Consistency
                if is_scaling_decision_consistent(full_name, desired_replicas) and desired_replicas != 0:
                    logger.info(f"Scaling decision for {full_name} is consistent: Scale by {desired_replicas}, Top Features: {feature_importance}")
                    if current_replicas != desired_replicas:
                        logger.info(f"Triggering autoscaler API for {full_name} to scale to {desired_replicas}")
                        trigger_autoscaler_api(full_name, desired_replicas, feature_importance)
                        scaling_decision_history[full_name] = []
                else:
                    logger.info(f"Scaling decision for {full_name} is not yet consistent: {scaling_decision_history[full_name]}")

        except Exception as e:
            logger.error(f"Error in main loop: {e}")

        logger.debug(f"Sleeping for {SCAN_INTERVAL} seconds before next scan.")
        time.sleep(SCAN_INTERVAL)
        
def initialize_model(full_name: str):
    """Initializes the selected model for a given deployment."""
    global models
    if MODEL_TYPE == "decision_tree":
        models[full_name] = DecisionTreeRegressor()
    elif MODEL_TYPE == "gradient_boosting":
        models[full_name] = GradientBoostingRegressor()
    elif MODEL_TYPE == "arima":
        models[full_name] = None  # ARIMA doesn't need initialization here
    else:
        models[full_name] = None  # Rule-based doesn't need a model
    logger.info(f"Model initialized for deployment: {full_name} using {MODEL_TYPE}")

def get_metrics(custom_objects_api, deployment_name: str, namespace: str, selector: Dict[str, str]) -> Tuple[int, int]:
    """Gets the average CPU and RAM usage for a deployment."""
    logger.debug(f"Fetching metrics for deployment: {namespace}/{deployment_name}")
    label_selector = ",".join([f"{k}={v}" for k, v in selector.items()])
    
    try:
        metrics = custom_objects_api.list_namespaced_custom_object(
            group="metrics.k8s.io",
            version="v1beta1",
            namespace=namespace,
            plural="pods",
            label_selector=label_selector,
        )
    except ApiException as e:
        logger.error(f"Failed to fetch metrics for deployment {namespace}/{deployment_name}: {e}")
        return 0, 0

    total_cpu = 0
    total_ram = 0
    pod_count = 0
    for pod_metric in metrics.get("items", []):
        pod_count += 1
        for container in pod_metric.get("containers", []):
            cpu_usage = container["usage"]["cpu"]
            ram_usage = container["usage"]["memory"]
            if cpu_usage.endswith("n"):
                total_cpu += int(cpu_usage[:-1]) / 1e6  # Convert nanocores to millicores
            elif cpu_usage.endswith("m"):
                total_cpu += int(cpu_usage[:-1])  # Already in millicores
            if ram_usage.endswith("Ki"):
                total_ram += int(ram_usage[:-2]) / 1024  # Convert Ki to Mi
            elif ram_usage.endswith("Mi"):
                total_ram += int(ram_usage[:-2])  # Already in Mi

    if pod_count > 0:
        avg_cpu = int(total_cpu / pod_count)
        avg_ram = int(total_ram / pod_count)
        logger.debug(f"Average metrics for {namespace}/{deployment_name}: CPU={avg_cpu}, RAM={avg_ram}")
        return avg_cpu, avg_ram
    else:
        logger.warning(f"No pods found for deployment: {namespace}/{deployment_name}")
        return 0, 0

def update_history(full_name: str, cpu_usage: int, ram_usage: int):
    """Updates the CPU and RAM usage history for a deployment."""
    global cpu_history, ram_history
    cpu_history[full_name].append(cpu_usage)
    ram_history[full_name].append(ram_usage)
    if len(cpu_history[full_name]) > CPU_HISTORY_LENGTH:
        cpu_history[full_name].pop(0)
    if len(ram_history[full_name]) > CPU_HISTORY_LENGTH:
        ram_history[full_name].pop(0)

def update_scaling_decision_history(full_name: str, decision: int):
    """Updates the scaling decision history for a deployment."""
    global scaling_decision_history
    scaling_decision_history[full_name].append(decision)
    if len(scaling_decision_history[full_name]) > CONSISTENCY_THRESHOLD:
        scaling_decision_history[full_name].pop(0)

def is_scaling_decision_consistent(full_name: str, decision: int) -> bool:
    """Checks if the scaling decision is consistent for the required threshold."""
    global scaling_decision_history
    history = scaling_decision_history[full_name]
    return len(history) == CONSISTENCY_THRESHOLD and all(d == decision for d in history)

def make_scaling_decision(full_name: str, current_replicas: int) -> Tuple[int, Dict[str, float]]:
    """Makes a scaling decision using the selected model."""
    global models, cpu_history, ram_history

    if len(cpu_history[full_name]) < CPU_HISTORY_LENGTH:
        logger.warning(f"Not enough data to make scaling decision for {full_name}. Required: {CPU_HISTORY_LENGTH}, Available: {len(cpu_history[full_name])}")
        return current_replicas, {}  # Not enough data for prediction

    if MODEL_TYPE == "decision_tree" or MODEL_TYPE == "gradient_boosting":
        # Prepare data for the model
        X = np.arange(len(cpu_history[full_name])).reshape(-1, 1)
        y = np.array(cpu_history[full_name])
        model = models[full_name]
        model.fit(X, y)  # Train the model
        predicted_cpu = model.predict([[len(cpu_history[full_name])]])[0]
    elif MODEL_TYPE == "arima":
        # Use ARIMA for prediction
        model = ARIMA(cpu_history[full_name], order=(5, 1, 0))
        model_fit = model.fit()
        predicted_cpu = model_fit.forecast()[0]
    else:
        # Rule-based scaling
        predicted_cpu = cpu_history[full_name][-1]

    # Scaling Logic
    if predicted_cpu > SCALE_UP_THRESHOLD:
        # Scale proportionally based on how far the predicted CPU is above the threshold
        scale_factor = predicted_cpu / SCALE_UP_THRESHOLD
        desired_replicas = int(current_replicas * scale_factor)
    elif predicted_cpu < SCALE_DOWN_THRESHOLD:
        # Scale proportionally based on how far the predicted CPU is below the threshold
        scale_factor = SCALE_DOWN_THRESHOLD / max(predicted_cpu, 1)  # Avoid division by zero
        desired_replicas = max(1, int(current_replicas / scale_factor))  # Ensure replicas don't go below 1
    else:
        desired_replicas = current_replicas

    # Simplified Feature Importance
    feature_importance = calculate_feature_importance(cpu_history[full_name], predicted_cpu)
    return desired_replicas, top_n_features(feature_importance, 3)
def calculate_feature_importance(cpu_history: List[int], predicted_cpu: float) -> Dict[str, float]:
    """Calculates a simplified feature importance."""
    feature_importance = {}
    for i, val in enumerate(cpu_history):
        importance = abs(val - predicted_cpu)
        feature_importance[f"t-{CPU_HISTORY_LENGTH - 1 - i}"] = importance
    logger.debug(f"Feature importance calculated: {feature_importance}")
    return feature_importance

def top_n_features(feature_importance: Dict[str, float], n: int) -> Dict[str, float]:
    """Returns the top N features based on importance."""
    sorted_importance = sorted(feature_importance.items(), key=lambda item: abs(item[1]), reverse=True)
    return dict(sorted_importance[:n])

def trigger_autoscaler_api(full_name: str, desired_replicas: int, feature_importance: Dict[str, float]):
    """Triggers scaling via an API call to the autoscaler."""
    namespace, deployment_name = full_name.split("/")
    # Convert np.float32 to native Python float
    feature_importance = {key: float(value) for key, value in feature_importance.items()}
    
    payload = {
        "namespace": namespace,
        "deployment": deployment_name,
        "instructReplicas": desired_replicas,
        "feature_importance": feature_importance,
        "call-from": "AI Scaling Agent",
        "model_type": MODEL_TYPE,
    }
    logger.debug(f"Triggering autoscaler API with payload: {payload}")
    try:
        response = requests.post(AUTOSCALER_API_URL, json=payload)
        response.raise_for_status()
        logger.info(f"Scaling API call successful: {response.text}")
    except requests.exceptions.RequestException as e:
        logger.error(f"Error calling autoscaler API: {e}")  
            
if __name__ == '__main__':
    main()