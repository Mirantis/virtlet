# Provision a Kubernetes Cluster with GKE

GKE is a hosted Kubernetes by Google. GKE clusters can be provisioned using a single command:

```
gcloud container clusters create craft
```

GKE clusters can be customized and supports different machine types, number of nodes, and network settings. 

## Create a Kubernetes cluster using gcloud

```
gcloud container clusters create craft \
  --disk-size 200 \
  --enable-cloud-logging \
  --enable-cloud-monitoring \
  --machine-type n1-standard-1 \
  --num-nodes 3 
```
