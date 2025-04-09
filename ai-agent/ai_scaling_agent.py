import os
 import time
 from kubernetes import client, config, watch
 from kubernetes.client import api_client
 import numpy as np
 import tensorflow as tf  # For LSTM
 from collections import defaultdict
 from typing import List, Dict, Tuple
 import requests
 import json

 # Configuration (adjust as needed)
 CPU_HISTORY_LENGTH = 10
 SCAN_INTERVAL = 15  # How often to scan Deployments (seconds) - Faster for demo
 AUTOSCALER_API_URL = os.environ.get("AUTOSCALER_API_URL", "http://your-autoscaler-service/scale")  # Replace with your autoscaler API URL
 SCALE_UP_THRESHOLD = 80  # CPU percentage to trigger scale-up
 SCALE_DOWN_THRESHOLD = 20  # CPU percentage to trigger scale-down
 SCALE_BUFFER = 5  # Percentage buffer to prevent flapping

 # Global Variables
 cpu_history: Dict[str, List[int]] = defaultdict(list)  # Key: "namespace/deployment_name", Value: list of CPU values
 lstm_models: Dict[str, tf.keras.Model] = {}  # Key: "namespace/deployment_name", Value: LSTM model
 model_metadata: Dict[str, Dict[str, any]] = {}  # Store model info (accuracy, etc.)

 def main():
     # 1. Kubernetes Client Setup
     try:
         config.load_incluster_config()
     except config.ConfigException:
         config.load_kube_config()
     core_v1 = client.CoreV1Api()
     apps_v1 = client.AppsV1Api()
     metrics_v1beta1 = client.CustomObjectsApi()

     # 2. Main Loop
     while True:
         # 3. Discover Deployments
         try:
             deployments = apps_v1.list_all_namespaces_deployment()
         except Exception as e:
             print(f"Error listing deployments: {e}")
             time.sleep(SCAN_INTERVAL)
             continue

         for deployment in deployments.items:
             if deployment.metadata.namespace != "kube-system":
                 namespace = deployment.metadata.namespace
                 deployment_name = deployment.metadata.name
                 full_name = f"{namespace}/{deployment_name}"

                 # 4. Get Metrics
                 try:
                     cpu_usage = get_cpu_usage(apps_v1, metrics_v1beta1, deployment_name, namespace)
                     print(f"CPU Usage for {full_name}: {cpu_usage}")
                 except Exception as e:
                     print(f"Error getting CPU usage for {full_name}: {e}")
                     continue

                 # 5. Preprocess Data and Update History
                 update_cpu_history(full_name, cpu_usage)

                 # 6. Initialize LSTM Model (if needed)
                 if full_name not in lstm_models:
                     initialize_lstm_model(full_name)

                 # 7. Make Scaling Decision (LSTM)
                 desired_replicas, feature_importance = make_scaling_decision(full_name)

                 # 8. Trigger Scaling (API Call)
                 if desired_replicas != 0:
                     print(
                         f"Scaling decision for {full_name}: Scale by {desired_replicas}, Top Features: {feature_importance}"
                     )
                     trigger_autoscaler_api(full_name, desired_replicas, feature_importance)

         time.sleep(SCAN_INTERVAL)

 def initialize_lstm_model(full_name: str):
     """Initializes an LSTM model for a given deployment."""
     global lstm_models, model_metadata
     lstm_models[full_name] = tf.keras.models.Sequential([
         tf.keras.layers.LSTM(16, input_shape=(CPU_HISTORY_LENGTH, 1), activation='relu', return_sequences=True),  # Increased units, return sequences
         tf.keras.layers.LSTM(8, activation='relu'),
         tf.keras.layers.Dense(1, activation='relu')
     ])
     lstm_models[full_name].compile(optimizer='adam', loss='mean_squared_error', metrics=['mae'])

     # Store model metadata (for demo purposes)
     model_metadata[full_name] = {
         "architecture": "LSTM (2 layers)",
         "units_layer1": 16,
         "units_layer2": 8,
         "activation": "relu",
         "optimizer": "adam",
         "loss_function": "mean_squared_error",
         "initial_accuracy": 0.0,  # Placeholder - you'd calculate this during training
     }

 def get_cpu_usage(apps_v1: client.AppsV1Api, metrics_v1beta1: client.CustomObjectsApi, deployment_name: str, namespace: str) -> int:
     """Gets the average CPU usage for a deployment."""

     deployment = apps_v1.read_namespaced_deployment(name=deployment_name, namespace=namespace)
     selector = ",".join([f"{k}={v}" for k, v in deployment.spec.selector.match_labels.items()])
     metrics = metrics_v1beta1.list_namespaced_pod_metric(namespace=namespace, label_selector=selector)

     total_cpu = 0
     pod_count = 0
     for pod_metric in metrics['items']:
         pod_count += 1
         for container in pod_metric['containers']:
             cpu_usage_ns = container['usage']['cpu']
             cpu_usage_cores = int(cpu_usage_ns) / 1e9
             total_cpu += cpu_usage_cores * 1000

     if pod_count > 0:
         return int(total_cpu / pod_count)
     else:
         return 0

 def update_cpu_history(full_name: str, cpu_usage: int):
     """Updates the CPU usage history for a deployment."""
     global cpu_history
     cpu_history[full_name].append(cpu_usage)
     if len(cpu_history[full_name]) > CPU_HISTORY_LENGTH:
         cpu_history[full_name].pop(0)

 def make_scaling_decision(full_name: str) -> Tuple[int, Dict[str, float]]:
     """Makes a scaling decision using LSTM and calculates feature importance."""

     global lstm_models, cpu_history, model_metadata

     if len(cpu_history[full_name]) < CPU_HISTORY_LENGTH:
         return 0, {}  # Not enough data for prediction

     input_data = np.array(cpu_history[full_name]).reshape(1, CPU_HISTORY_LENGTH, 1)
     predicted_cpu = lstm_models[full_name].predict(input_data, verbose=0)[0][0]

     # 1. Train the model
     lstm_models[full_name].train_on_batch(input_data, input_data) # Train on the input itself

     # 2. Simplified Feature Importance (Example)
     feature_importance = calculate_feature_importance(cpu_history[full_name], predicted_cpu)

     # 3. Scaling Logic (using global thresholds)
     desired_replicas = 0
     if predicted_cpu > SCALE_UP_THRESHOLD:
         desired_replicas = 1
     elif predicted_cpu < SCALE_DOWN_THRESHOLD:
         desired_replicas = -1

     # Update model accuracy (placeholder - replace with actual calculation)
     model_metadata[full_name]["initial_accuracy"] = 0.85  # Example value

     return desired_replicas, top_n_features(feature_importance, 3)

 def calculate_feature_importance(cpu_history: List[int], predicted_cpu: float) -> Dict[str, float]:
     """Calculates a simplified feature importance."""

     feature_importance: Dict[str, float] = {}
     if not cpu_history:
         return feature_importance

     for i, val in enumerate(cpu_history):
         # Simple difference for importance
         importance = abs(val - predicted_cpu)
         feature_importance[f"t-{CPU_HISTORY_LENGTH - 1 - i}"] = float(importance)

     return feature_importance

 def top_n_features(feature_importance: Dict[str, float], n: int) -> Dict[str, float]:
     """Returns the top N features based on importance."""
     sorted_importance = sorted(feature_importance.items(), key=lambda item: abs(item[1]), reverse=True)
     return dict(sorted_importance[:n])

 def trigger_autoscaler_api(full_name: str, desired_replicas: int, feature_importance: Dict[str, float]):
     """Triggers scaling via an API call to the autoscaler."""

     namespace, deployment_name = full_name.split("/")
     payload = {
         "namespace":      namespace,
         "deployment_name": deployment_name,
         "desired_replicas": desired_replicas,
         "feature_importance": feature_importance,  # Send feature importance (optional)
     }
     try:
         response = requests.post(AUTOSCALER_API_URL, json=payload)
         response.raise_for_status()
         print(f"Scaling API call successful: {response.text}")
     except requests.exceptions.RequestException as e:
         print(f"Error calling autoscaler API: {e}")

 if __name__ == '__main__':
     main()