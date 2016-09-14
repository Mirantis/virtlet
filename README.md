# virtlet

[![Join the chat at https://gitter.im/Mirantis/virtlet](https://badges.gitter.im/Mirantis/virtlet.svg)](https://gitter.im/Mirantis/virtlet?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)

Virtlet is a Kubernetes runtime server which allows you to run VM workloads, based on QCOW2 images.

## Setting up Go project

It's expected that you will place this project in `$GOPATH/src/github.com/Mirantis/virtlet` to have a valid import paths.

Updating dependencies also needs additional tools which could be installed using:

```sh
curl https://glide.sh/get | sh
go get github.com/sgotti/glide-vc
```

## Running local environment

Flannel network requires additional settings to be prepared in host `etcd` database. They can be set from command line using:

```sh
$ etcdctl set /coreos.com/network/config '{ "Network": "10.1.0.0/16", "Backend": { "Type": "host-gw" } }'
```

To run local environment, please install [docker-compose](https://pypi.python.org/pypi/docker-compose)
at least in 1.8.0 version. If your Linux distribution is providing an older version, we suggest to
use Python virtualenv(wrapper):

```sh
apt-get install virtualenvwrapper
mkvirtualenv docker-compose
pip install docker-compose
```

If you have docker-compose ready to use, you can set up the virtlet dev environment by doing:

```sh
cd contrib/docker-compose
docker-compose up -e IFACE=eth0
```

where `eth0` will be interface used for inter node communication.

Then please go to the sources of Kubernetes:

```sh
cd $GOPATH/k8s.io/kubernetes
```

You will need to checkout the following [branch and fork](https://github.com/nhlfr/kubernetes/tree/syncpod-virtlet).
Unfortunately, it contains the needed codebase which is still in review in upstream. As soon as these
commits will be merged, we will suggest you to use upstream Kubernetes code.

After that, you can run a local cluster which will talk to virtlet:

```sh
export KUBERNETES_PROVIDER=local
export CONTAINER_RUNTIME_ENDPOINT=/run/virtlet.sock
./hack/local-up-cluster.sh

Flannel network requires additional settings to be prepared in host `etcd` database. They can be set from command line using:

```sh
$ etcdctl set /coreos.com/network/config '{ "Network": "10.1.0.0/16", "Backend": { "Type": "host-gw" } }'
```
