# Cloud init support

Virtlet is using [cloud-init](https://cloudinit.readthedocs.io/en/latest/) to configure VMs. As always you can check [Virtlet documentation](../cloud-init-data-generation.md) to see exactly how Virtlet uses it.

Cloud-init data is passed to the VM using Pod's annotations.

The most common use case is to pass SSH public key to the VM:

```yaml
VirtletSSHKeys: |
      ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCaJEcFDXEK2ZbX0ZLS1EIYFZRbDAcRfuVjpstSc0De8+sV1aiu+dePxdkuDRwqFtCyk6dEZkssjOkBXtri00MECLkir6FcH3kKOJtbJ6vy3uaJc9w1ERo+wyl6SkAh/+JTJkp7QRXj8oylW5E20LsbnA/dIwWzAF51PPwF7A7FtNg9DnwPqMkxFo1Th/buOMKbP5ZA1mmNNtmzbMpMfJATvVyiv3ccsSJKOiyQr6UG+j7sc/7jMVz5Xk34Vd0l8GwcB0334MchHckmqDB142h/NCWTr8oLakDNvkfC1YneAfAO41hDkUbxPtVBG5M/o7P4fxoqiHEX+ZLfRxDtHB53 me@localhost
```

or it can be pulled from [Secret](https://kubernetes.io/docs/concepts/configuration/secret/):

```yaml
VirtletSSHKeySource: secret/mysecret
```

It's also possible to create a new user by defining user data:

```yaml
    VirtletCloudInitUserData: |
      ssh_pwauth: True
      users:
      - name: testuser
        gecos: User
        primary-group: testuser
        groups: users
        lock_passwd: false
        shell: /bin/bash
        # the password is "testuser"
        passwd: "$6$rounds=4096$wPs4Hz4tfs$a8ssMnlvH.3GX88yxXKF2cKMlVULsnydoOKgkuStTErTq2dzKZiIx9R/pPWWh5JLxzoZEx7lsSX5T2jW5WISi1"
        sudo: ALL=(ALL) NOPASSWD:ALL
        ssh-authorized-keys:
           ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCaJEcFDXEK2ZbX0ZLS1EIYFZRbDAcRfuVjpstSc0De8+sV1aiu+dePxdkuDRwqFtCyk6dEZkssjOkBXtri00MECLkir6FcH3kKOJtbJ6vy3uaJc9w1ERo+wyl6SkAh/+JTJkp7QRXj8oylW5E20LsbnA/dIwWzAF51PPwF7A7FtNg9DnwPqMkxFo1Th/buOMKbP5ZA1mmNNtmzbMpMfJATvVyiv3ccsSJKOiyQr6UG+j7sc/7jMVz5Xk34Vd0l8GwcB0334MchHckmqDB142h/NCWTr8oLakDNvkfC1YneAfAO41hDkUbxPtVBG5M/o7P4fxoqiHEX+ZLfRxDtHB53 me@localhost
```

It's also possible to use [ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) as a source for user data:

```yaml
VirtletCloudInitUserDataSource: configmap/vm-user-data
```

When you are passing [Environment variables](../environment-variables.md) to a Pod Virtlet uses cloud-init to pass it to a VM and store them in a `/etc/cloud/environment` file.
When you are using ConfigMap or Secret in a Pod then they are passed to the VM using cloud-init by creating new files there. Pod's volumes are also converted to `mounts` and mounted in VM using cloud-init when listed in the `volumeMounts` field.

See `virtlet/examples/k8s.yaml` where `VirtletCloudInitUserData` is used to do some advanced scripting there.
