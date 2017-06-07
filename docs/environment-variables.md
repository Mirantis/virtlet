# Environment variables support

Virtlet supports passing environment variables to VM by using
[cloud-init](http://cloudinit.readthedocs.io/en/latest/index.html).
The environment variables set for the container the pod definition, along with
several other predefined by kubernetes environment, are written to
`/etc/cloud/environment` file. It can be sourced/used by any application.
Format of this file is the same as in standard in linux `/etc/environment`
file.
