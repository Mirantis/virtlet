# Troubleshooting Virtlet installation

Q: I've used `virtletctl gen | kubectl apply -f -` to install Virtlet but looks like there is no Virtlet pod in `kube-system` namespace, while Virtlet DaemonSet is present in `kube-system` namespace.

A: Most likely you didn't mark nodes for Virtlet with `extraRuntime=virtlet` label.

Q: I have running Virtlet runtime pods in `kube-system` namespace but when I try to run any of the examples, it fails to start.

A: Please validate installation and configuration of CRI Proxy according to its [documentation](https://github.com/Mirantis/criproxy#installation)

Q: I've already installed CRI Proxy and Virtlet, but when I try to start an example pod it fails to start with `ErrImagePull` status.

A: Please ensure that `kubelet` is [configured to use CRI Proxy as a CRI runtime](https://github.com/Mirantis/criproxy#reconfiguring-kubelet-to-use-cri-proxy).

Q: I have CRI Proxy installed and `kubelet` configured to use CRI Proxy but the pod hangs with `ContainerCreating` status.

A: Please ensure that you have installed Virtlet using `virtletctl gen | kubectl apply -f -`.

Q: I've gone through all of the above suggestions but still, I have problems running Virtlet runtime or any VM pods.

A: Reach us on [Virtlet channel on Kubernetes Slack](https://kubernetes.slack.com/messages/virtlet/) or file a [GitHub issue](https://github.com/Mirantis/virtlet/issues).
