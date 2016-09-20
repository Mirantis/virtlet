# virtlet

Virtlet is a Kubernetes runtime server which allows you to run VM workloads, based on QCOW2 images.

## Setting up Go project

It's expected that you will place this project in `$GOPATH/src/github.com/Mirantis/virtlet` to have a valid import paths.

Updating dependencies also needs additional tools which could be installed using:

```sh
curl https://glide.sh/get | sh
go get github.com/sgotti/glide-vc
```

## Running local environment

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
docker-compose up
```

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
```
