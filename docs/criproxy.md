# CRI Proxy

CRI Proxy makes it possible to run several CRI implementations on the
same node. It also includes `dockershim` (CRI->docker interface) which
is currently imported from Kubernetes code. CRI Proxy isn't tied to
Virtlet in the sense that it can be used with other runtimes,
too. `dockershim` usage is also optional.

## How CRI Proxy works

Below is a diagram depicting the way CRI Proxy works. The basic idea
is forwarding the requests to different runtimes based on prefixes of
image name / pod id / container id prefixes.

![CRI Request Path](criproxy.png)

Let's say CRI proxy is started as follows:
```
/usr/local/bin/criproxy -v 3 -alsologtostderr -connect docker,virtlet.cloud:/run/virtlet.sock
```

`-v 3 -alsologtostderr` options here may be quite useful for
debugging, because they make CRI proxy log detailed info about every
CRI request going through it, including any errors and the result.

`-connect docker,virtlet.cloud:/run/virtlet.sock` specifies the list of
runtimes that the proxy passes requests to.

The `docker` part is a special case, meaning that `criproxy` must
start in-process `dockershim` and use it as the primary (prefixless)
runtime (this is used by the bootstrap procedure, in normal
installations `dockershim` runs as a separate process, although using
same `criproxy` binary).  It's also possible to specify other primary
runtime instead, e.g. `/run/some-other-runtime.sock`.

`virtlet.cloud:/run/virtlet.sock` denotes an alternative runtime
socket. This means that image service requests that include image
names starting with `virtlet.cloud/` must be directed to the CRI
implementation listening on a Unix domain socket at
`/run/virtlet.sock`. Pods that need to run on `virtlet.cloud` runtime must
have `virtlet.cloud` as the value of `kubernetes.io/target-runtime`
annotation.

There can be any number of runtimes, although probably using more than
a couple of runtimes is a rare use case.

Here's an example of a pod that needs to run on `virtlet.cloud` runtime:
```
apiVersion: v1
kind: Pod
metadata:
  name: cirros-vm
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
    scheduler.alpha.kubernetes.io/affinity: >
      {
        "nodeAffinity": {
          "requiredDuringSchedulingIgnoredDuringExecution": {
            "nodeSelectorTerms": [
              {
                "matchExpressions": [
                  {
                    "key": "extraRuntime",
                    "operator": "In",
                    "values": ["virtlet"]
                  }
                ]
              }
            ]
          }
        }
      }
spec:
  containers:
    - name: cirros-vm
      image: virtlet.cloud/image-service/cirros
```

First of all, there's `kubernetes.io/target-runtime: virtlet.cloud`
annotation that directs `RunPodSandbox` requests to `virtlet.cloud` runtime.

There's also `nodeAffinity` spec that makes the pod run only on the
nodes that have `extraRuntime=virtlet` label. This is not required
by CRI proxy mechanism itself and is related to deployment mechanism
being used (more on this in CRI Proxy bootstrap section below).

Another important part is `virtlet.cloud/image-service/cirros` image name.
It means that the image is handled by `virtlet.cloud` runtime and actual
image name passed to the runtime is `image-service/cirros`. In case of
virtlet this means downloading QCOW2 image from
`http://image-service/cirros`.

In order to distinguish between runtimes during requests that don't
include image name or pod annotations such as `RemovePodSandbox`, CRI
proxy adds prefixes to pod and container ids returned by the runtimes.

## CRI Proxy bootstrap procedure

In order to make it easier to set up environments with support for
multiple CRI implementations on all or some of the nodes, CRI proxy
includes 'bootstrap' mechanism. It involves starting proxy in a
container that's automatically restarted if the node or docker daemon
restarts and dynamically reconfiguring kubelet to use CRI proxy's
endpoint for RuntimeService and ImageService. Below is a diagram that
shows a node that runs Virtlet and CRI proxy that were installed using
this bootstrap procedure.

![CRI Proxy and Virtlet after Bootstrap](bootstrap.png)

As of now the process is tailored for test environments. For
production environments it's preferable to deploy CRI proxy by other
means, e.g. by installing a package on the node or adding
a service to systemd that starts CRI proxy in a container.

Here's step-by-step description of CRI proxy bootstrap procedure
in case of Virtlet.

1. Kubelets on the nodes that are going to be used with CRI proxy
   must have `--feature-gates=DynamicKubeletConfig` command line flag.
   Apiserver must have `--feature-gates=StreamingProxyRedirects=true`
   flag that's necessary to support Exec/Attach in CRI (needed for
   docker-shim in this case).
2. Add 'extraRuntime=virtlet' label to the nodes that will be used for virtlet:
   ```
   kubectl label node NODE_NAME extraRuntime=virtlet
   ```
   This label is used by node affinity settings of Virlet DaemonSet.
   It can also be used to set node affinity of VM pods.
3. User loads Virtlet deployment yaml:
   ```
   kubectl create -f deploy/virtlet-ds.yaml
   ```
   The yaml file includes Virtlet DaemonSet and a ServiceAccount object used by
   CRI proxy bootstrap procedure to access apiserver.
4. DaemonSet's init container checks for saved node info file file,
   `/etc/criproxy/node.conf`. If this file exists, the bootstrap
   procedure is already done (or is not required) so the rest of this
   sequence is skipped, init container exits with status 0 and Virtlet
   pod starts on the node.
5. DaemonSet's init container drops `criproxy` binary under
   `/opt/criproxy/bin` on the host and starts it with `-install` flag,
   starting the proxy installer.
6. DaemonSet's init container invokes `criproxy` binary with `-grab`
   flag. In this mode `criproxy` determines PID of kubelet by looking
   at the process which currently listens on Unix domain socket named
   `/var/run/dockershim.sock`. It then gets command line arguments
   from `/proc/NNN/cmdline` where `NNN` is PID of kubelet. It then
   stores kubelet command line together with other info (node name and
   docker endpoint) as `/etc/criproxy/node.conf`.
7. DaemonSet's init container starts invokes `criproxy` binary in
   installer mode (with `-install` flag).
8. The installer patches kubelet config to enable CRI and use CRI proxy
   RuntimeService and ImageService endpoints. This is equivalent to
   adding the following options to kubelet:
   ```
   --experimental-cri --container-runtime=remote \
   --container-runtime-endpoint=unix:///run/criproxy.sock \
   --image-service-endpoint=unix:///run/criproxy.sock
   ```
9. The installer starts CRI proxy container with 'Always' restart
   policy, so it will be restarted in case if docker daemon gets
   restarted or the machine gets rebooted. It then waits for
   `/run/criproxy.sock` Unix domain socket to become connectable.
   Note that the container uses nsenter to break out of
   its namespaces, so Docker is being used as poor man's
   process manager here in order to avoid systemd dependency.
10. The installer creates configmap named `kubelet-NODENAME` containing
    a JSON object with patched kubelet config under `kubelet.config`
    key. This causes kubelet to restart and pick up the new config.
11. When the new kubelet process makes its first request to CRI proxy,
    the proxy scans Docker for containers that were started by kubelet
    before switching to CRI proxy and removes them, because some
    containers may be affected by the transition.
