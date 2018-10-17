## Virtlet provisioning
#### Using MCP

Follow the latest instructions how to [deploy Kubernetes cluster](https://docs.mirantis.com/mcp/latest/mcp-deployment-guide/deploy-mcp-cluster-using-drivetrain/deploy-k8s.html) and then deploy Virtlet [Deploy Virtlet on MCP](https://docs.mirantis.com/mcp/latest/mcp-deployment-guide/deploy-mcp-cluster-manually/deploy-kubernetes-cluster-manually/enable-virtlet/deploy-virtlet.html)

#### Using kubeadm-dind-cluster integration

For this workshop you will use [kubeadm-dind-cluster](https://github.com/kubernetes-sigs/kubeadm-dind-cluster) cluster.
Use `demo.sh` script which will deploy Kubernetes and provision Virtlet there.

```bash
MULTI_CNI=1 ./deploy/demo.sh
```

Answer `y` to the script’s questions and wait until the script completes. The script will create a CirrOS VM for you and display its shell prompt.


#### Other deployment options

To see how to provision Virtlet on non MCP clusters see the [deployment docs](../../../deploy/real-cluster.md)
