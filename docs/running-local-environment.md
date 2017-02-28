# Running local environment for Virtlet

## Pre run steps

At this stage Virtlet have following requirements:

* SELinux/AppArmor disabled on host (to disable them - follow documentation from your host Linux distribution),
* if host have libvirt installed - it should be stopped when working with Virtlet,
* [docker](https://www.docker.com) should be installed on host and user account on which Virtlet will be built and run - should be properly configured to use this docker installation (possibly adding user's account into group in which docker deamon is running should be enough, but please follow docker documentation for your host Linux distribution),
* you need a kubernetes cluster with specified version which is pinned in glide.yaml file


## Running local environment

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

## Kubernetes environment

Currently the only supported version of Kubernetes is specified in glide.yaml file, Virtlet may work with different version though.

Assuming standard configuration for Kubernetes sources location, use following commands:

```sh
cd $GOPATH/src/k8s.io/kubernetes
export KUBERNETES_PROVIDER=local
export CONTAINER_RUNTIME=remote
export CONTAINER_RUNTIME_ENDPOINT=/run/virtlet.sock
./hack/local-up-cluster.sh
```

## Cleanup

Between multiple runs of local environment (especially during development process)
it may be useful to invoke cleanup procedure which removes any residues from
abrupt stopping of virtlet+libvirt container.
It may be invoked by adding `-e LIBVIRT_CLEANUP=true` to `docker run` command
line flags when starting the container.
