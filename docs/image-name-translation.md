# Image Name Translation

By default, the image URL is encoded into image name as described in [Image Handling](images.md).
However, due to the strict rules for the image name format such approach has number of significant restrictions:

* Colon cannot appear in the name. Thus the URL cannot include scheme part (`http://` or `https://`). As a consequence
  it becomes impossible use images that have scheme that differs from configured default.
* For the same reasons it is impossible to use URLs that include queries, authentication credentials or port number.
* The URL must be all low-case which works well for the domain part, but may not be acceptable for the path part.

To overcome these limitations, Virtlet provides a mechanism for image name translation.
The idea is that image can be identified by some abstract ID rather than URL. Virtlet then will map this ID
to arbitrary URL using special translation table that specifies rules for image name translation.

Thus instead of `virtlet.cloud/example.net/path/to/my.qcow2` one would use `virtlet.cloud/my-image` and put a mapping that says that
`my-image` must be translated to `http://example.net/path/to/my.qcow2` into translation table.
Here and below I assume that `CRI Proxy` is used. Otherwise, the `virtlet.cloud/` prefix is not needed.

## Translation configs

The translation table is built from arbitrary number of translation configs. The config has the following format:

```yaml
prefix: my-prefix
translations:
- name: cirros
  url: https://download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img
- name: ubuntu/16.04
  url: https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img
- regexp: 'cirros/(\d\.\d\.\d)'
  url: 'https://download.cirros-cloud.net/$1/cirros-$1-x86_64-disk.img'
- regexp: 'centos/(\d+)-(\d+)'
  url: 'https://cloud.centos.org/centos/$1/images/CentOS-$1-x86_64-GenericCloud-$2.qcow2'
```

The prefix is optional and may be omitted. In example above the image name
`virtlet.cloud/my-prefix/cirros` is going to be translated into `https://download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img`,
but `virtlet.cloud/cirros` won't be unless there is also a translation config without a prefix (or with empty string prefix).
In the later case, the `cirros` part will be treated as an URL (the default behavior without translations).

There are two types of translations: those that map a fixed image name and those that map set of names identified by regexp expression.
In the later case, URL can be generalized by using regexp sub-matches through the `$n` syntax.
In example above, `virtlet.cloud/my-prefix/centos/7-01` is going to be translated to
`https://cloud.centos.org/centos/$1/images/CentOS-7-x86_64-GenericCloud-01.qcow2`.

The regexp translations are only available when Virtlet is run with `IMAGE_REGEXP_TRANSLATION` environment variable set to a
non-empty value, which is not the case by default.

Fixed name translations has a higher precedence than regexp ones. Thus for ambiguous names, fixed name translations are
always preferred.

## Provision of translation configs

There are two ways how translation configs can be delivered to Virtlet:

1. Through static YAML files
2. Through custom Kubernetes resource `VirtletImageMapping`

In the first case the translation configs are read from the yaml files from a directory in the `virtlet` container.
There are many ways, how files can be put into `virtlet` container. Default Virtlet tool-set uses `ConfigMap`-based
volume to mount `deploy/images.yaml` into `/etc/virtlet/images` path in container. The path is provide to Virtlet
through the `-image-translations-dir` CLI flag. The flag is optional, and, when omitted, completely disables file-based
translation configs.

With the second method, the configs are provided through custom Kubernetes resource `VirtletImageMapping` which looks as following:

```yaml
apiVersion: "virtlet.k8s/v1"
kind: VirtletImageMapping
metadata:
  name: primary
  namespace: kube-system
spec:
  prefix: ""
  translations:
   - ...
   - ...
```

where a translation config is placed into `spec` field and wrapped with usual Kubernetes metadata.
One can use `kubectl create -f mappings.yaml` to create such resources. But for this to be possible `VirtletImageMapping` resource kind
must be registered in Kubernetes. Virtlet does it on the first run. This such mappings cannot be created in the Kubernetes cluster that
never had Virtlet running.

There can be any number of `VirtletImageMapping` resource. However, currently all such mappings must be in the `kube-system` namespace.
`VirtletImageMapping` resource have a precedence over file-based configs for ambiguous image names. Thus it is convenient to put
defaults into static config files and then override them with `VirtletImageMapping` resources when needed.

## Configure HTTP transport for image download

By default, the image downloader uses default transport settings: system-wide CA certificates for HTTPS URLs,
up to 9 redirects and proxy from the `HTTP_PROXY`/`HTTPS_PROXY` environment variables. However, with image translation
configs it is possible to override these default and provide custom transport configuration.

Transport settings are grouped into profiles, each with the name and bunch of configuration settings. Each translation
rule may optionally have `transport` attribute set to profile name to be used for the image URL of that rule.
Below is an example of translation config that has all possible transport settings though all of them are optional:

```yaml
translations:
- name: mySmallImage
  url: https://my.host.loc/small.qcow2
  transport: my-server
- name: myImage
  url: https://my.host.loc/big.qcow2
  transport: my-server
transports:
  my-server:
    timeout: 30000  # in ms. 0 = no timeout (default)
    maxRedirects: 1 # at most 1 redirect allowed (i.e. 2 HTTP requests). null or missing value = any number of redirects
    proxy: http://my-proxy.loc:8080
    tls: # optional TLS settings. Use default system settings when not specified
      certificates: # there can be any mumber of certificates. Both CA and client certificates are put here
      - cert: |
         -----BEGIN CERTIFICATE-----
         # CA PEM block goes here
         # CA certificates are recognized by IsCA:TRUE flag in the certificate. Private key is not needed in this case
         # CA certificates are appended to the Linux system-wide list
         -----END CERTIFICATE-----

      - cert: |
         -----BEGIN CERTIFICATE-----
         # Client-based authentication certificate PEM block goes here
         # There can be several certificates put together if they share a single key
         -----END CERTIFICATE-----

        key: |
         -----BEGIN RSA PRIVATE KEY-----
         # PEM-encoded private key
         # for certificate-based client authentication private key must be present
         # Also the key is not required if it already contained in the cert PEM
         -----END RSA PRIVATE KEY-----

      serverName: my.host.com # because the certificate is for .com but we're connecting to .loc
      insecure: false         # when true, no server certificate validation is going to be performed
```

When no transport profile is specified for translation rule, the default system settings are used. However,
since the default value for `transport` attribute is an empty string, defining profile with empty name can
be used to override this default for all images in that particular config:

```yaml
translations:
- name: mySmallImage
  url: https://my.host.loc/small.qcow2
- name: myImage
  url: https://my.host.loc/big.qcow2
transports:
  "":
    proxy: http://my-proxy.loc:8080 # proxy for all images without explicit transport name
```

Of course, the same settings can be put into `VirtletImageMapping` objects:

```yaml
apiVersion: "virtlet.k8s/v1"
kind: VirtletImageMapping
metadata:
  name: primary
  namespace: kube-system
spec:
  translations:
  - name: mySmallImage
    url: https://my.host.loc/small.qcow2
  - name: myImage
    url: https://my.host.loc/big.qcow2
  transports:
    "":
      proxy: http://my-proxy.loc:8080 # proxy for all images without explicit transport name
```
