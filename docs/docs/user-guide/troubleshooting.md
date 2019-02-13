# Troubleshooting Virtlet installation

Q: I've used `virtletctl gen | kubectl apply -f -` to install Virtlet but looks like there is no Virtlet pod in `kube-system` namespace.

A: Most probably you missed to mark nodes for Virtlet with `extraRuntime=virtlet` label.

Q: I have running Virtlet runtime pods in `kube-system` namespace but when I'm trying to start any example - it fails to start.

A: Please validate installation and configuration of CRI Proxy according to its [documentation](https://github.com/Mirantis/criproxy#installation)

Q: I've already installed CRI Proxy and Virtlet but if I'm trying to start any example pod fails to start with status `ErrImagePull`.

A: Please ensure that `kubelet` is [configured to use CRI Proxy as a CRI runtime](https://github.com/Mirantis/criproxy#reconfiguring-kubelet-to-use-cri-proxy).

Q: I have CRI Proxy installed and `kubelet` configured to use CRI Proxy but pod hangs on `ContainerCreating` status.

A: Please ensure that you have installed Virtlet using `virtletctl gen | kubectl apply -f -`.

Q: I have Virtlet DaemonSet visible in `kube-system` namespace but it didn't started any Virtlet runtime pods.

A: That could have place due to missing `extraRuntime=virtlet` label on all nodes.

Q: I've validated all above descriptions but still have an issue with starting Virtlet runtime/pod using Virtlet runtime.

A: Reach us on [Virtlet channel on Kubernetes Slack](https://kubernetes.slack.com/messages/virtlet/) or fill an issue on our [bugtracker](https://github.com/Mirantis/virtlet/issues).
