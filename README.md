# virtlet [![Build Status](https://travis-ci.org/Mirantis/virtlet.svg?branch=master)](https://travis-ci.org/Mirantis/virtlet)

Virtlet is a Kubernetes runtime server which allows you to run VM workloads, based on QCOW2 images.

At this stage (pre-alpha), only supported way of starting Virtlet is following instructions from **Running local environment**.

## Usage

### Pre run steps

At this stage Virtlet have following requirements:

* SELinux/AppArmor disabled on host (to disable them - follow documentation from your host Linux distribution),
* if host have libvirt installed - it should be stopped when working with Virtlet,
* [docker](https://www.docker.com) should be installed on host and user account on which Virtlet will be built and run - should be properly configured to use this docker installation (possibly adding user's account into group in which docker deamon is running should be enough, but please follow docker documentation for your host Linux distribution),
* you need a kubernetes cluster with specified version which is pinned in glide.yaml file

### Networking support

For now the only supported configuration is the use of [CNI plugins](https://github.com/containernetworking/cni). To use custom CNI configuration, mount your `/etc/cni` and optionally `/opt/cni` into the virtlet container.
Virtlet have the same behavior and default values for `--cni-bin-dir` and `--cni-conf-dir` as described in kubelet network plugins [documentation](http://kubernetes.io/docs/admin/network-plugins/).

### Running local environment

First, you need to build virtlet binaries and the docker image:
```
build/cmd.sh clean
build/cmd.sh build
build/cmd.sh copy
docker build -t virtlet .
```

To run the virtlet container, use

```
docker run -it --rm --privileged --network=host \
       -e VIRTLET_LOGLEVEL=2 \
       -v /sys/fs/cgroup:/sys/fs/cgroup \
       -v /boot:/boot:ro \
       -v /lib/modules:/lib/modules:ro \
       -v /var/lib/viret:/var/lib/virtlet \
       -v /run:/run \
       virtlet
```

In the command above, `VIRTLET_LOGLEVEL` sets logging level for virtlet.
3 is currently highest verbosity level, the default being 2.

Fedora based systems require additional parameter for docker: `--pid=host`.

Now you can follow instructions from the next section.

### Kubernetes environment

Currently the only supported version of Kubernetes is specified in glide.yaml file, Virtlet may work with different version though.

Assuming standard configuration for Kubernetes sources location, use following commands:

```sh
cd $GOPATH/k8s.io/kubernetes
export KUBERNETES_PROVIDER=local
export CONTAINER_RUNTIME=remote
export CONTAINER_RUNTIME_ENDPOINT=/run/virtlet.sock
./hack/local-up-cluster.sh
```

### Running tests

To run integration & e2e tests, please install [docker-compose](https://pypi.python.org/pypi/docker-compose)
at least in 1.8.0 version. If your Linux distribution is providing an older version, we suggest to
use [virtualenvwrapper](https://virtualenvwrapper.readthedocs.io):

```sh
apt-get install virtualenvwrapper
mkvirtualenv docker-compose
pip install docker-compose
```

In order to run the tests, use:

```sh
./test.sh
```

### Virtlet usage demo

You can watch sample usage session under [this](https://asciinema.org/a/1aq4f2wd8lgw0e1yexvf1knmv) link.
**NOTE:** The demo was prepared using older virtlet version, it will be updated soon.
