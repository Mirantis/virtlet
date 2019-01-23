# Injecting files into the VM

Virtlet makes it possible to write set of files to the root filesystem of a VM using
[Config Map](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
or [Secret](https://kubernetes.io/docs/concepts/configuration/secret/)
as a source of data.

## Key-value conventions

ConfigMap or Secret should contain keys and values according to the
following convention:
```yaml
entry: content
entry_path: encoded/path/in/filesystem
entry_encoding: encoding_of_content
second_entry: content
second_entry_path: encoded/path/in/filesystem
second_entry_encoding: encoding_of_content
```

where `entry` is an arbitrary name, `entry_name` contains the destination
path on the VM root filesystem, and optional `entry_encoding`
denotes the encoding of the file content which can be `plain` for plain text
(for use in ConfigMaps) or `base64` (the default).

## ConfigMap example

Create a ConfigMap like this:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-files-set
data:
  locale: LANG="pl_PL.UTF-8"
  locale_path: /etc/locale.conf
  locale_encoding: plain
  host_conf: bXVsdGkgb2ZmCg==
  host_conf_path: /etc/host.conf
EOF
```

and then add it to a pod using the following annotation:
```yaml
...
metadata:
  ...
  annotations:
    VirtletFilesFromDataSource: configmap/my-files-set
```

## Secret example

Same data as show above can be specified via a `Secret`:

```bash
mkdir data
echo 'LANG="pl_PL.UTF-8"' >data/locale
echo /etc/locale.conf >data/locale_path
echo multi off >data/host_conf
echo /etc/host.conf >data/host_conf_path
kubectl create secret generic my-files-set --from-file=data/
rm -r data/
```

or recreating above using yaml notation (note: secrets use base64 encoding for
each value stored under each key):

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: my-files-set
data:
  locale: TEFORz0icGxfUEwuVVRGLTgiCg==
  locale_path: L2V0Yy9sb2NhbGUuY29uZgo=
  host_conf: bXVsdGkgb2ZmCg==
  host_conf_path: L2V0Yy9ob3N0LmNvbmYK
EOF
```

The secret can be injected into the root filesystem like that:

```yaml
...
metadata:
  ...
  annotations:
    VirtletFilesFromDataSource: secret/my-files-set
```
