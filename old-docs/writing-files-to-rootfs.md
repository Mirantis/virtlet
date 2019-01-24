# Writing files to root filesystem before VM boot

Virtlet makes it possible to write set of files to the root filesystem of a VM using
[Config Map](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
or [Secret](https://kubernetes.io/docs/concepts/configuration/secret/)
as a source of data.

## Usage

ConfigMap or Secret should consist of set of data in format:

```
entry: content
entry_path: encoded/path/in/filesystem
entry_encoding: encoding_of_content
second_entry: content
second_entry_path: encoded/path/in/filesystem
second_entry_encoding: encoding_of_content
```

where `entry` is any name, `entry_name` contains path where file should be
writen on VM root filesystem, optional `entry_encoding` denotes encoding of
file `content` which can be `plain` for plain text (for use in ConfigMaps)
or (default if such key is ommited) `base64`.

### ConfigMap

Create ConfigMap like:

```yaml
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
```

apply it:


```
kubectl apply --filename=path/to/above_file.yaml
```

then add the following to pod annotations: `VirtletFilesFromDataSource: configmap/my-files-set`.

### Secret

Same data as show above can be reused as `Secret` using:

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

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-files-set
data:
  locale: TEFORz0icGxfUEwuVVRGLTgiCg==
  locale_path: L2V0Yy9sb2NhbGUuY29uZgo=
  host_conf: bXVsdGkgb2ZmCg==
  host_conf_path: L2V0Yy9ob3N0LmNvbmYK
```

then use that in pod definition by setting: `VirtletFilesFromDataSource: secret/my-files-set`.
