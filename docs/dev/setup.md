## Prerequisites

You'll need the following to run the local environment:

* SELinux/AppArmor disabled on the host (to disable them, follow the
  documentation ofthe Linux distribution that you run on the host)
* if host have libvirt installed, it should be stopped when working
  with Virtlet
* [Docker](https://www.docker.com) should be installed on the host and
  the user account on which Virtlet will be built and run should be
  properly configured to use that Docker daemon (likely adding the
  user account into the group in which Docker deamon is running should
  be enough, but please follow the Docker documentation for your Linux
  distribution),
* [kubeadm-dind-cluster](https://github.com/kubernetes-sigs/kubeadm-dind-cluster/)
  script for Kubernetes version 1.12 (`dind-cluster-v1.9.sh`).
  
You can get the cluster startup script like this:
```
$ wget -O ~/dind-cluster-v1.12.sh https://github.com/kubernetes-sigs/kubeadm-dind-cluster/releases/download/v0.1.0/dind-cluster-v1.12.sh
$ chmod +x ~/dind-cluster-v1.12.sh
```

## Running the local environment

In order to start locally-built Virtlet and CRI proxy on `kubeadm-dind-cluster`: 
```
$ # Remove any previous build container & related docker volumes
$ # This is optional step that should be done if the build container
$ # doesn't match current Virtlet sources (i.e. is too old)
$ build/cmd.sh clean

$ # build Virtlet binaries & the image
$ build/cmd.sh build

$ # start DIND cluster
$ ~/dind-cluster-v1.12.sh up

$ # copy binaries to kube-node-1
$ build/cmd.sh copy-dind

$ # inject Virtlet image into the DIND node and start the Virtlet DaemonSet
$ build/cmd.sh start-dind

$ # run some e2e tests
$ build/cmd.sh e2e -test.v

$ # run e2e tests that have 'Should have default route' in their description
$ build/cmd.sh e2e -test.v -ginkgo.focus="Should have default route"

$ # Restart the DIND cluster. Binaries from copy-dind are preserved
$ # (you may copy newer ones with another copy-dind command)
$ ~/dind-cluster-v1.12.sh up

$ # start Virtlet daemonset again
$ build/cmd.sh start-dind
```

You may use [flannel](https://github.com/coreos/flannel) instead of
default CNI bridge networking for the test cluster. To do so,
set `CNI_PLUGIN` environment variable:
```
$ export CNI_PLUGIN=flannel
```

Virtlet uses KVM if it's available by default. To disable it for your
local development environment, set `VIRTLET_DISABLE_KVM` environment
variable to a non-empty value before running `build/cmd.sh start-dind`.
