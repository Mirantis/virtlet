# [cri-tools](https://github.com/kubernetes-incubator/cri-tools) compatibility status

cri-tools project utilizes [ginkgo](https://onsi.github.io/ginkgo) package, which provides means for setup/teardown, organizing tests in groups, and flags for running/skipping specific subsets of tests.

## Summary of validation by Groups (Specs)

| Test Spec Name | Overall number | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| Container            | 7  | 7  | 5  |
| Image Manager  | 6  | 4  | 4  |
| Networking         | 3  | 0  | 0  |
| PodSandbox       | 3  | 3  | 3  |
| Runtime info       | 2   |  2  | 2  |
| Security Context | 12 |  4   | 0  |
| Streaming           | 3   | 0   | 0  |
| **overall**           | 36 | 20 | 14 |

Use `Spec names` from the first column above to run specific subsets of tests:
`# critest --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock --focus="Container" validation`

## critest validation result details

### "Container" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| creating container  | create, list |  y | y |
| starting container   | create, start |  y | y |
| stopping container | create, start, stop |  y | y |
| removing container | create, remove |  y | y |
| execSync | check execSync | y | n |
| container with volume | create container with hostDir |  y | y |
| container with log | start container with LogPath | y | n |

### "Image Manager" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| image with tag | pull image by ref| y | y |
| image without tag| pull image by name| y | y |
| image with digest | pull image by digestRef | y | y |
| get image| get image status | y | y |
| exactly 3 image | tags | n | n |
| exactly 3 repoTags | tags | n | n |

### "Networking" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| support DNS config| check /etc/resolv.conf content | n | n |
| port mapping with only container port | | n | n |
| port mapping with host port and container port | | n | n |

### "PodSandbox" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| running PodSandbox | run sandbox, list | y | y |
| stopping PodSandbox | run sandbox, stop | y | y |
| removing PodSandbox| run sandbox, stop, remove | y | y |

### "Runtime info" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| runtime info | get runtime version | y | y |
| runtime conditions | get runtime status | y | y |

### "Security Context" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| support HostPID | created "sandbox" with nginx and "busybox" containers with with hostPID; nginx pid must be seen from within busybox; using execSync for checking | n | n |
| HostIpc is true | check shared memory segment is included in the "busybox" container created with hostIPC set; using execSync for check | n | n |
| HostIpc is false | the same as above but check that memory is not included | n | n |
| HostNetwork is true|  | n | n |
| HostNetwork is false | | n | n |
| support RunAsUser | execSync check | y | n | 
| support RunAsUserName | execSync check | y | n | 
| ReadOnlyRootfs is false | | y | n |
| ReadOnlyRootfs is true | | y | n |
| Privileged is true| | ? | n |
| Privileged is false| | ? | n |
| setting Capability| | ? | n |

### "Streaming" Spec:
| Test Spec Name | Short description | Compatible with virtlet | Passed |
| -----------------------|:---------------------:|:---------------------------:|:----------:|
| support exec | | y | n |
| support attach | | y | n |
| support portforward | | n | n |

## critest running steps
1.  To be able to run virtlet compatible tests you need to fix following issues:
    1. Currently CRI-tools uses hardcoded "busybox" and "nginx" image names for tests. So need to change them on cirros url.
    1. Virtlet adds ids to domain’s name and cri-tools also adds id and prefix, what leads to error on domain creation:

          > Monitor path /var/lib/libvirt/qemu/domain-ceb27ab2-385b-574b-54cc-90a9db9e92be-container-for-start-test-7916763f-5b2e-11e7-87bc-52540070019e/monitor.sock too big for destination'

     Need to apply `cri-tools.patch` and build `cri-tools` inside `virtlet-builder`:

```
build/cmd.sh stop
build/cmd.sh run "mkdir /go/src/github.com/kubernetes-incubator ; \
                  cd /go/src/github.com/kubernetes-incubator ; \
                  git clone https://github.com/kubernetes-incubator/cri-tools.git && cd cri-tools ; \
                  git apply ../../Mirantis/virtlet/cri-tools.patch ; \
                 make binaries && make install;  "
```

2. Setup `virtlet-build` container to be ready to run `critest`:
```
build/cmd.sh run "mkdir -p /usr/libexec/kubernetes/kubelet-plugins/volume/exec ; \
                  mkdir -p /var/lib/kubelet/pods ; \
                  cp ./_output/flexvolume_driver /flexvolume_driver ; \
                  cp ./_output/virtlet /usr/local/bin/ ; "
build/cmd.sh run "VIRTLET_DISABLE_KVM=1 /start.sh > ./virtlet-cri-rools.log 2>&1 &"
build/cmd.sh vsh
critest --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock --focus="Container" validation
```

## crictl usage example:

```
# cat sandbox-config.json
{
    "metadata": {
        "name": "cirros-sandbox",
        "namespace": "default",
        "attempt": 1,
        "uid": "hdishd83djaidwnduwk28bcsb"
    },
    "linux": {
    }
}
# cat container-config.json
{
  "metadata": {
      "name": "cirros-vm"
  },
  "image":{
      "image": "download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img"
  },
  "linux": {
  }
}

#
# crictl --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock sandbox run ./vm-sandbox.json
hdishd83djaidwnduwk28bcsb
#
# crictl --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock sandbox ls
SANDBOX ID	NAME	STATE
hdishd83djaidwnduwk28bcsb	cirros-sandbox	SANDBOX_READY

#
# crictl --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock image pull download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img
download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img

# virsh vol-list --pool default
 Name                 Path
------------------------------------------------------------------------------
 10da1bf07c27b64768ed07b798095f8d779bdbc3_cirros-0.3.5-x86_64-disk.img /var/lib/libvirt/images/10da1bf07c27b64768ed07b798095f8d779bdbc3_cirros-0.3.5-x86_64-disk.img

# crictl --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock container create hdishd83djaidwnduwk28bcsb ./vm-container.json ./vm-sandbox.json
264e1739-7b6a-5a3d-564c-baae69b5bdb0

# virsh vol-list --pool volumes
 Name                 Path
------------------------------------------------------------------------------
 root_264e1739-7b6a-5a3d-564c-baae69b5bdb0 /var/lib/virtlet/root_264e1739-7b6a-5a3d-564c-baae69b5bdb0

root@3ef5de7d492b:/go/src/github.com/Mirantis/virtlet# virsh list --all
 Id    Name                           State
----------------------------------------------------
 -     264e1739-7b6a-5a3d-564c-baae69b5bdb0-cirros-vm shut off

# crictl --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock container start 264e1739-7b6a-5a3d-564c-baae69b5bdb0
264e1739-7b6a-5a3d-564c-baae69b5bdb0

# crictl --runtime-endpoint=/run/virtlet.sock --image-endpoint=/run/virtlet.sock container ls
CONTAINER ID	CREATED	STATE	NAME
264e1739-7b6a-5a3d-564c-baae69b5bdb0	2017-06-26 18:44:36.437995153 +0000 UTC	CONTAINER_RUNNING	cirros-vm

# virsh list --all
 Id    Name                           State
----------------------------------------------------
 1     264e1739-7b6a-5a3d-564c-baae69b5bdb0-cirros-vm running

```
