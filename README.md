# virtlet [![Build Status](https://travis-ci.org/Mirantis/virtlet.svg?branch=master)](https://travis-ci.org/Mirantis/virtlet)

Virtlet is a Kubernetes runtime server which allows you to run VM workloads, based on QCOW2 images.

At this stage (pre-alpha), it's possible to run Virtlet by following the instructions from either [Running local environment](docs/running-local-environment.md) or [Deploy using DaemonSets](deploy/README.md) documents.

[See here](docs/architecture.md) for the description of Virtlet architecture.

## Getting started with Virtlet

To try out Virtlet follow the instructions from [Running local environment](docs/running-local-environment.md) and [try out examples](examples/README.md) documents.

### Virtlet introduction video

You can watch and listen to Virtlet introduction and description video that was recorded on Kubernetes Community Meeting [here](https://youtu.be/x5uBq-ugoio?t=38).

### Virtlet usage demo

You can watch sample usage session under [this](https://asciinema.org/a/1a6xp5j4o22rnsx9wpvumd4kt) link.

You can also give Virtlet a quick try using our demo script (requires Docker 1.12+):
```
wget https://cdn.rawgit.com/Mirantis/virtlet/ivan4th/kubeadm-dind-cluster-deployment/deploy/demo.sh
chmod +x demo.sh
./demo.sh
```

The demo will start a test cluster, deploy Virtlet on it and then boot a [CirrOS](https://launchpad.net/cirros) VM there. You may access sample nginx server via `curl http://nginx.default.svc.cluster.local` from inside the VM. To detach from VM console, press `Ctrl-]`. After you detach from the VM you can remove the test cluster with `./dind-cluster-v1.5.sh clean`.

The demo is based on [kubeadm-dind-cluster](https://github.com/Mirantis/kubeadm-dind-cluster) project. **Docker btrfs storage driver is currently unsupported.** Please refer to `kubeadm-dind-cluster` documentation for more info.

## Need any help with Virtlet?

If you will encounter any issue when using Virtlet please look into our [issue tracker](http://github.com/Mirantis/virtlet/issues) on github. If your case is not mentioned there - please fill new issue for it.

## Contributing

Virtlet is an open source project and any contributions are welcomed. Look into [Contributing guidelines](CONTRIBUTING.md) document for our guidelines and further instructions on how to set up Virtlet development environment.

## Licensing

Unless specifically noted, all parts of this project are licensed under the [Apache 2.0 license](LICENSE).

