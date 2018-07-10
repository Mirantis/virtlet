# Diagnostics

Virtlet provides a set of
[virtletctl diag](virtletctl/virtletctl_diag.md) commands that can
help with troubleshooting. The diagnostics can be invoked either
directly or buy means of a
[Sonobuoy](https://github.com/heptio/sonobuoy) plugin.

## Direct invocation

The most basic diagnostics command is [virtletctl diag dump](virtletctl/virtletctl_diag_dump.md):
```
$ virtletctl diag out/
$ ls -lR out
total 0
drwxr-xr-x  3 user  wheel  96 Jul 11 01:56 nodes

out/nodes:
total 0
drwxr-xr-x  12 user  wheel  384 Jul 11 01:56 kube-node-1

out/nodes/kube-node-1:
total 5352
-rwxr-xr-x  1 user  wheel  1276000 Jul 11 01:56 criproxy.log
-rwxr-xr-x  1 user  wheel     1787 Jul 11 01:56 ip-a.txt
-rwxr-xr-x  1 user  wheel      322 Jul 11 01:56 ip-r.txt
drwxr-xr-x  3 user  wheel       96 Jul 11 01:56 libvirt-logs
drwxr-xr-x  5 user  wheel      160 Jul 11 01:56 libvirt-xml
-rwxr-xr-x  1 user  wheel     9964 Jul 11 01:56 metadata.txt
-rwxr-xr-x  1 user  wheel     1443 Jul 11 01:56 netns.txt
-rwxr-xr-x  1 user  wheel     9217 Jul 11 02:56 psaux.txt
-rwxr-xr-x  1 user  wheel    18214 Jul 11 01:56 stack.log
-rwxr-xr-x  1 user  wheel    64314 Jul 11 01:56 virtlet-pod-libvirt.log
-rwxr-xr-x  1 user  wheel  1349763 Jul 11 01:56 virtlet-pod-virtlet.log

out/nodes/kube-node-1/libvirt-logs:
total 8
-rwxr-xr-x  1 user  wheel  2172 Jul 11 01:56 virtlet-1b2261ca-7ed6-cirros-vm.log

out/nodes/kube-node-1/libvirt-xml:
total 24
-rwxr-xr-x  1 user  wheel  3511 Jul 11 01:56 domain-virtlet-1b2261ca-7ed6-cirros-vm.xml
-rwxr-xr-x  1 user  wheel   445 Jul 11 01:56 pool-volumes.xml
-rwxr-xr-x  1 user  wheel  1041 Jul 11 01:56 volume-virtlet_root_1b2261ca-7ed6-58e7-58de-0eef2c9d5320.xml
```

The following files and directories are produced for each Kubernetes
node that runs Virtlet:
* `criproxy.log` - the logs of CRI Proxy's systemd unit
* `ip-a.txt` - the output of `ip a` on the node
* `ip-r.txt` - the output of `ip r` on the node
* `metadata.txt` - the contents of Virtlet's internal metadata db in a text form
* `netns.txt` - the output of `ip a` and `ip r` for each network
  namespace that's managed by Virtlet
* `psaux.txt` - the output of `ps aux` command on the node
* `stack.log` - the dump of Go stack of Virtlet process
* `virtlet-pod-libvirt.log` - the log of Virtlet pod's libvirt container
* `virtlet-pod-virtlet.log` - the log of Virtlet pod's virtlet container
* `livirt-logs` - a directory with libvirt/QEMU logs for each domain
* `libvirt-xml` - the dumps of all the domains, storage pools and storage volumes in libvirt

It's also possible to dump Virtlet diagnostics as JSON to stdout using
`virtletctl diag dump --json`. The JSON file can be subsequently
unpacked into the aforementioned directory structure using
[virtletctl diag unpack](virtletctl/virtletctl_diag_unpack.md).

## Sonobuoy

Virtlet diagnostics can be run as a
[Sonobuoy](https://github.com/heptio/sonobuoy) plugin.  Unfortunately,
right now Sonobuoy's plugin support is
[somewhat limited](https://github.com/heptio/sonobuoy/issues/405). Because
of that problem, Sonobuoy run must be done in two phases, first
generating YAML and then using `virtletctl` to patch it (inject
Virtlet sonobuoy plugin):
```
$ cat sonobuoy.json
{
  "plugins": [ { "name": "virtlet" } ]
}
$ sonobuoy gen --config sonobuoy.json --e2e-focus nosuchtest |
    virtletctl diag sonobuoy |
    kubectl apply -f -
$ # wait till sonobuoy run is complete
$ sonobuoy status
PLUGIN  STATUS          COUNT
virtlet complete        1

Sonobuoy has completed. Use `sonobuoy retrieve` to get results.
$ sonobuoy retrieve
```

The diagnostics results are placed under `plugins/virtlet/results` and
can be unpacked using [virtletctl diag unpack](virtletctl/virtletctl_diag_unpack.md):
```
$ virtletctl diag unpack out/ <sonobuoy_output_dir/plugins/virtlet/results
```
