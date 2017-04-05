# Running local environment for Virtlet

## Pre-run steps

At this stage Virtlet have following requirements:

* SELinux/AppArmor disabled on host (to disable them - follow documentation from your host Linux distribution),
* if host have libvirt installed - it should be stopped when working with Virtlet,
* [docker](https://www.docker.com) should be installed on host and user account on which Virtlet will be built and run - should be properly configured to use this docker installation (possibly adding user's account into group in which docker deamon is running should be enough, but please follow docker documentation for your host Linux distribution),
* [kubeadm-dind-cluster](https://github.com/Mirantis/kubeadm-dind-cluster) for version 1.5 (`dind-cluster-v1.5.sh`).
  You can get the cluster startup script like this:
```
$ wget -O ~/dind-cluster-v1.5.sh https://cdn.rawgit.com/Mirantis/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.5.sh
$ chmod +x ~/dind-cluster-v1.5.sh
```

## Running local environment

In order to start locally-built Virtlet and CRI proxy on `kubeadm-dind-cluster`: 
```
$ build/cmd.sh clean
$ build/cmd.sh build

$ # start DIND cluster
$ ~/dind-cluster-v1.5.sh up

$ # copy binaries to kube-node-1
$ build/cmd.sh copy-dind

$ # start Virtlet daemonset
$ build/cmd.sh start-dind

$ # Restart DIND cluster. Binaries from copy-dind are preserved
$ # (you may copy newer ones with another copy-dind command)
$ ~/dind-cluster-v1.5.sh up

$ # start Virtlet daemonset again
$ build/cmd.sh start-dind
```

You can also build Virtlet image and propagate it to the DIND node
(by default the latest image from Docker Hub is used with current
binaries being placed instead of original ones):
```
$ build/cmd.sh copy
$ docker build -t mirantis/virtlet .

$ # copy the image to the DIND node
$ docker save mirantis/virtlet | docker exec -i kube-node-1 docker load
```

You may use [flannel](https://github.com/coreos/flannel) instead of
default CNI bridge networking for the test cluster. To do so,
set `CNI_PLUGIN` environment variable:
```
$ export CNI_PLUGIN=flannel
```
