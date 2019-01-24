## virtletctl

Virtlet control tool

**Synopsis**


virtletctl provides a number of utilities for Virtet-enabled
Kubernetes cluster.


**Subcommands**

* [virtletctl diag](#virtletctl-diag) - Virtlet diagnostics
* [virtletctl gen](#virtletctl-gen) - Generate Kubernetes YAML for Virtlet deployment
* [virtletctl gendoc](#virtletctl-gendoc) - Generate Markdown documentation for the commands
* [virtletctl install](#virtletctl-install) - Install virtletctl as a kubectl plugin
* [virtletctl ssh](#virtletctl-ssh) - Connect to a VM pod using ssh
* [virtletctl validate](#virtletctl-validate) - Make sure the cluster is ready for Virtlet deployment
* [virtletctl version](#virtletctl-version) - Display Virtlet version information
* [virtletctl virsh](#virtletctl-virsh) - Execute a virsh command
* [virtletctl vnc](#virtletctl-vnc) - Provide access to the VNC console of a VM pod
## virtletctl diag

Virtlet diagnostics

**Synopsis**

Retrieve and unpack Virtlet diagnostics information


**Subcommands**

* [virtletctl diag dump](#virtletctl-diag-dump) - Dump Virtlet diagnostics information
* [virtletctl diag sonobuoy](#virtletctl-diag-sonobuoy) - Add Virtlet sonobuoy plugin to the sonobuoy output
* [virtletctl diag unpack](#virtletctl-diag-unpack) - Unpack Virtlet diagnostics information
## virtletctl diag dump

Dump Virtlet diagnostics information

**Synopsis**

Pull Virtlet diagnostics information from the nodes and dump it as a directory tree or JSON

```
virtletctl diag dump output_dir [flags]
```


**Options**


```
--json
```
Use JSON output
## virtletctl diag sonobuoy

Add Virtlet sonobuoy plugin to the sonobuoy output

**Synopsis**

Find and patch sonobuoy configmap in the yaml that's read from stdin to include Virtlet sonobuoy plugin

```
virtletctl diag sonobuoy [flags]
```


**Options**


```
--tag string
```
Set virtlet image tag for the plugin
## virtletctl diag unpack

Unpack Virtlet diagnostics information

**Synopsis**

Read Virtlet diagnostics information as JSON from stdin and unpacks into a directory tree

```
virtletctl diag unpack output_dir [flags]
```

## virtletctl gen

Generate Kubernetes YAML for Virtlet deployment

**Synopsis**

This command produces YAML suitable for use with kubectl apply -f -

```
virtletctl gen [flags]
```


**Options**


```
--compat
```
Produce YAML that's compatible with older Kubernetes versions

```
--crd
```
Dump CRD definitions only

```
--dev
```
Development mode for use with kubeadm-dind-cluster

```
--tag string
```
Set virtlet image tag
## virtletctl gendoc

Generate Markdown documentation for the commands

**Synopsis**

This command produces documentation for the whole command tree, or the Virtlet configuration data.

```
virtletctl gendoc output_dir [flags]
```


**Options**


```
--config
```
Produce documentation for Virtlet config
## virtletctl install

Install virtletctl as a kubectl plugin

**Synopsis**


This command install virtletctl as a kubectl plugin.

After running this command, it becomes possible to run virtletctl
via 'kubectl plugin virt'.

```
virtletctl install [flags]
```

## virtletctl ssh

Connect to a VM pod using ssh

**Synopsis**


This command runs ssh and makes it connect to a VM pod.


```
virtletctl ssh [flags] user@pod -- [ssh args...]
```

## virtletctl validate

Make sure the cluster is ready for Virtlet deployment

**Synopsis**

Check configuration of the cluster nodes to make sure they're ready for Virtlet deployment

```
virtletctl validate [flags]
```

## virtletctl version

Display Virtlet version information

**Synopsis**

Display information about virtletctl version and Virtlet versions on the nodes

```
virtletctl version [flags]
```


**Options**


```
--client
```
Print virtletctl version only

```
-o, --output string
```
One of 'text', 'short', 'yaml' or 'json'
 **(default value:** `"text"`)

```
--short
```
Print just the version number(s) (same as -o short)
## virtletctl virsh

Execute a virsh command

**Synopsis**


This command executes libvirt virsh command.

A VM pod name in the form @podname is translated to the
corresponding libvirt domain name. If @podname is specified,
the target k8s node name is inferred automatically based
on the information of the VM pod. In case if no @podname
is specified, the command is executed on every node
and the output for every node is prepended with a line
with the node name and corresponding Virtlet pod name.

```
virtletctl virsh [flags] virsh_command -- [virsh_command_args...]
```


**Options**


```
--node string
```
the name of the target node
## virtletctl vnc

Provide access to the VNC console of a VM pod

**Synopsis**


This command forwards a local port to the VNC port used by the
specified VM pod. If no local port number is provided, a random
available port is picked instead. The port number is displayed
after the forwarding is set up, after which the commands enters
an endless loop until it's interrupted with Ctrl-C.


```
virtletctl vnc pod [port] [flags]
```


## Global options


```
--alsologtostderr
```
log to standard error as well as files

```
--as string
```
Username to impersonate for the operation

```
--as-group stringArray
```
Group to impersonate for the operation, this flag can be repeated to specify multiple groups.

```
--certificate-authority string
```
Path to a cert file for the certificate authority

```
--client-certificate string
```
Path to a client certificate file for TLS

```
--client-key string
```
Path to a client key file for TLS

```
--cluster string
```
The name of the kubeconfig cluster to use

```
--context string
```
The name of the kubeconfig context to use

```
-h, --help
```
help for virtletctl

```
--insecure-skip-tls-verify
```
If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure

```
--kubeconfig string
```
Path to the kubeconfig file to use for CLI requests.

```
--log-backtrace-at traceLocation
```
when logging hits line file:N, emit a stack trace
 **(default value:** `:0`)

```
--log-dir string
```
If non-empty, write log files in this directory

```
--logtostderr
```
log to standard error instead of files

```
-n, --namespace string
```
If present, the namespace scope for this CLI request

```
--password string
```
Password for basic authentication to the API server

```
--request-timeout string
```
The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests.
 **(default value:** `"0"`)

```
-s, --server string
```
The address and port of the Kubernetes API server

```
--stderrthreshold severity
```
logs at or above this threshold go to stderr
 **(default value:** `2`)

```
--token string
```
Bearer token for authentication to the API server

```
--user string
```
The name of the kubeconfig user to use

```
--username string
```
Username for basic authentication to the API server

```
-v, --v Level
```
log level for V logs

```
--virtlet-runtime string
```
the name of virtlet runtime used in kubernetes.io/target-runtime annotation
 **(default value:** `"virtlet.cloud"`)

```
--vmodule moduleSpec
```
comma-separated list of pattern=N settings for file-filtered logging
