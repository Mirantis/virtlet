# virtlet

Virtlet is a Kubernetes runtime server which allows you to run VM workloads, based on QCOW2 images.

At this stage (pre-alpha), only supported way of starting Virtlet is following instructions from **Running local environment**.

## Setting up Go project

Virtlet requires properly configured [golang installation](https://golang.org/doc/install).

It's expected that you will place this project in `$GOPATH/src/github.com/Mirantis/virtlet` to have a valid import paths.

Updating dependencies also needs additional tools which could be installed using:

```sh
curl https://glide.sh/get | sh
go get github.com/sgotti/glide-vc
```

## Usage

### Pre run steps

At this stage Virtlet have following requirements:

* SELinux/AppArmor disabled on host (to disable them - follow documentation from your host Linux distribution),
* if host have libvirt installed - it should be stopped when working with Virtlet,
* [docker](https://www.docker.com) should be installed on host and user account on which Virtlet will be built and run - should be properly configured to use this docker installation (possibly adding user's account into group in which docker deamon is running should be enough, but please follow docker documentation for your host Linux distribution),
* host should have `python` environment in version which is compatible with `docker-compose` (installation instructions in later section of this doc),
* at the moment Virtlet requires particular patches added into kubernetes, so follow next section to setup it.

### Running docker-compose environment

To run local environment, please install [docker-compose](https://pypi.python.org/pypi/docker-compose)
at least in 1.8.0 version. If your Linux distribution is providing an older version, we suggest to
use [virtualenvwrapper](https://virtualenvwrapper.readthedocs.io):

```sh
apt-get install virtualenvwrapper
mkvirtualenv docker-compose
pip install docker-compose
```

If you have `docker-compose` ready to use, you can build the Virtlet dev environment by doing:

```sh
cd $(REPOSITORY_BASE_DIR)/contrib/docker-compose
docker-compose build
```

assuming that `REPOSITORY_BASE_DIR` environment variable is pointing to directory containing Virtlet git clone.

For this moment, we only support local cluster configuration, starting of which instructions are in next section, but in future prepared in this way docker images can be distributed on worker nodes to speedup process of starting.

To start Virtlet environment use following commands:

```sh
cd $(REPOSITORY_BASE_DIR)/contrib/docker-compose
docker-compose up
```

Now you can follow instructions from next section.

### Kubernetes environment

Then please go to the sources of Kubernetes:

```sh
cd $GOPATH/k8s.io/kubernetes
```

You will need to checkout the following [branch from fork](https://github.com/nhlfr/kubernetes/tree/syncpod-virtlet).
It contains the needed codebase which is still in review in upstream. As soon as these
commits will be merged, we will suggest you to use upstream Kubernetes code.

After that, you can run a local cluster which will talk to Virtlet:

```sh
export KUBERNETES_PROVIDER=local
export CONTAINER_RUNTIME_ENDPOINT=/run/virtlet.sock
./hack/local-up-cluster.sh
```
