# Overview

Virtlet, as all Kuberentes runtimes, is using CNI plugins to configure network interface.
Thanks to [CNI-Genie](https://github.com/Huawei-PaaS/CNI-Genie), Virtlet can use multiple CNI plugins to configure networking in a pod which results in multiple interfaces.

# Requirements

* CNI Genie https://github.com/Huawei-PaaS/CNI-Genie
* pre-installed CNI plugins in version 0.6.0 or 0.7.1
* `cniVersion` set to at least `0.3.0` in all configuration files for CNI networks (see examples below)

## CNI-genie

Before you proceed, read the CNI Genie [documentation](https://github.com/Huawei-PaaS/CNI-Genie/blob/master/docs/CNIGenieFeatureSet.md)

There are two ways to tell CNI Genie which CNI networks to use:
* by setting `cni: <plugins list>` in pods annotation
* by setting the `default_plugin` option in CNI Genie configuration file

## Configuring networks
CNI plugins should return result in at least 0.3.0 format. All config files should be updated.

# Example deployment using [kdc](https://github.com/kubernetes-sigs/kubeadm-dind-cluster)

## Start a Kubernetes 1.9 cluster with Flannel, Calico, and CNI-Genie
```
wget https://cdn.rawgit.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.9.sh
chmod +x dind-cluster-v1.9.sh
```

Delete previous cluster if any:
```bash
./dind-cluster-v1.9.sh clean
```
Start a cluster with one worker node and install Flannel:
```bash
NUM_NODES=1 CNI_PLUGIN=flannel ./dind-cluster-v1.9.sh up
```
Install Calico:
```bash
kubectl apply -f https://docs.projectcalico.org/v2.6/getting-started/kubernetes/installation/hosted/kubeadm/1.6/calico.yaml
```
Install CNI-Genie:
```bash
kubectl apply -f https://raw.githubusercontent.com/Huawei-PaaS/CNI-Genie/master/conf/1.8/genie-plugin.yaml
```

## Update configuration files

(Optional) Set `"default_plugin": "calico,flannel"` in `/etc/cni/net.d/00-genie.conf`:
```bash
docker exec kube-node-1 bash -c "jq '.default_plugin=\"calico,flannel\"' /etc/cni/net.d/00-genie.conf > /tmp/genie.tmp && mv /tmp/genie.tmp /etc/cni/net.d/00-genie.conf"
```
Set `cniVersion=0.3.0` in Flannel, Calico and CNI-Genie configuration files:
```bash
docker exec kube-node-1 bash -c "jq '.cniVersion=\"0.3.0\"' /etc/cni/net.d/10-calico.conf > /tmp/calico.tmp && mv /tmp/calico.tmp /etc/cni/net.d/10-calico.conf"
docker exec kube-node-1 bash -c "jq '.cniVersion=\"0.3.0\"' /etc/cni/net.d/10-flannel.conflist > /tmp/flannel.tmp && mv /tmp/flannel.tmp /etc/cni/net.d/10-flannel.conflist"
docker exec kube-node-1 bash -c "jq '.cniVersion=\"0.3.0\"' /etc/cni/net.d/00-genie.conf > /tmp/genie.tmp && mv /tmp/genie.tmp /etc/cni/net.d/00-genie.conf"
```

## Install Virtlet
```bash
./build/cmd.sh build
./build/cmd.sh start-dind
```

## Run a test VM
```bash
kubectl create -f examples/ubuntu-multi-cni.yaml
```
Wait for a pod to be in `Ready` state. After that, you can attach to it and log in using the `testuser/testuser` credentials.
```bash
kubectl attach -it ubuntu-vm
```
Run `ip a` command. You should see two interfaces, for example:
```bash
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast state UP group default qlen 1000
    link/ether fe:42:99:6d:ec:75 brd ff:ff:ff:ff:ff:ff
    inet 192.168.135.129/24 brd 192.168.135.255 scope global eth0
       valid_lft forever preferred_lft forever
    inet6 fe80::fc42:99ff:fe6d:ec75/64 scope link
       valid_lft forever preferred_lft forever
3: eth1: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1450 qdisc pfifo_fast state UP group default qlen 1000
    link/ether 0a:58:0a:f4:01:06 brd ff:ff:ff:ff:ff:ff
    inet 10.244.1.6/24 brd 10.244.1.255 scope global eth1
       valid_lft forever preferred_lft forever
    inet6 fe80::858:aff:fef4:106/64 scope link tentative dadfailed
       valid_lft forever preferred_lft forever
```

# Example files

See below examples of the CNI configuration files after changes:

## CNI Genie
File `/etc/cni/net.d/00-genie.conf`:
```JSON
{
  "name": "k8s-pod-network",
  "type": "genie",
  "log_level": "info",
  "datastore_type": "kubernetes",
  "hostname": "kube-node-1",
  "policy": {
    "type": "k8s",
    "k8s_auth_token": "XYZ"
  },
  "kubernetes": {
    "k8s_api_root": "https://10.96.0.1:443",
    "kubeconfig": "/etc/cni/net.d/genie-kubeconfig"
  },
  "romana_root": "http://:",
  "segment_label_name": "romanaSegment",
  "default_plugin": "calico,flannel",
  "cniVersion": "0.3.0"
}
```
## Calico
File `/etc/cni/net.d/10-calico.conf`:
```JSON
{
  "name": "k8s-pod-network",
  "cniVersion": "0.3.0",
  "type": "calico",
  "etcd_endpoints": "http://10.96.232.136:6666",
  "log_level": "info",
  "mtu": 1500,
  "ipam": {
    "type": "calico-ipam"
  },
  "policy": {
    "type": "k8s",
    "k8s_api_root": "https://10.96.0.1:443",
    "k8s_auth_token": "XYZ"
  },
  "kubernetes": {
    "kubeconfig": "/etc/cni/net.d/calico-kubeconfig"
  }
}

```
## Flannel
File `/etc/cni/net.d/10-flannel.conflist`:
```JSON
{
  "name": "cbr0",
  "plugins": [
    {
      "type": "flannel",
      "delegate": {
        "hairpinMode": true,
        "isDefaultGateway": true
      }
    },
    {
      "type": "portmap",
      "capabilities": {
        "portMappings": true
      }
    }
  ],
  "cniVersion": "0.3.0"
}

```
# Troubleshooting

If you encounter any issues when configuring multiple interfaces, gather all logs from the Virtlet pod starting with `CNI Genie`.
