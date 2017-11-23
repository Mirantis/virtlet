# Running local environment for Virtlet

## Pre-run steps

At this stage Virtlet have following requirements:

* SELinux/AppArmor disabled on host (to disable them - follow documentation from your host Linux distribution),
* if host have libvirt installed - it should be stopped when working with Virtlet,
* [docker](https://www.docker.com) should be installed on host and user account on which Virtlet will be built and run - should be properly configured to use this docker installation (possibly adding user's account into group in which docker deamon is running should be enough, but please follow docker documentation for your host Linux distribution),
* [kubeadm-dind-cluster](https://github.com/Mirantis/kubeadm-dind-cluster) for version 1.8 (`dind-cluster-v1.7.sh`).
  You can get the cluster startup script like this:
```
$ wget -O ~/dind-cluster-v1.8.sh https://cdn.rawgit.com/Mirantis/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.8.sh
$ chmod +x ~/dind-cluster-v1.8.sh
```

## Running local environment

In order to start locally-built Virtlet and CRI proxy on `kubeadm-dind-cluster`: 
```
$ # Remove the previous build container & related docker volumes
$ # This is optional step that should be done if the build container
$ # doesn't match current Virtlet sources (i.e. is too old)
$ build/cmd.sh clean

$ # build Virtlet binaries & the image
$ build/cmd.sh build

$ # start DIND cluster
$ ~/dind-cluster-v1.8.sh up

$ # copy binaries to kube-node-1
$ build/cmd.sh copy-dind

$ # inject Virtlet image into the DIND node and start Virtlet daemonset
$ build/cmd.sh start-dind

$ # run some e2e tests
$ build/cmd.sh e2e -test.v

$ # run e2e tests that have 'Should have default route' in their description
$ build/cmd.sh e2e -test.v -ginkgo.focus="Should have default route"

$ # Restart DIND cluster. Binaries from copy-dind are preserved
$ # (you may copy newer ones with another copy-dind command)
$ ~/dind-cluster-v1.8.sh up

$ # start Virtlet daemonset again
$ build/cmd.sh start-dind
```

You may use [flannel](https://github.com/coreos/flannel) instead of
default CNI bridge networking for the test cluster. To do so,
set `CNI_PLUGIN` environment variable:
```
$ export CNI_PLUGIN=flannel
```

Note that KVM is disabled by default for the development environment.
In order to enable it, comment out `VIRTLET_DISABLE_KVM` environment
variable setting in `deploy/virtlet-ds.yaml` before doing
`build/cmd.sh start-dind`.
