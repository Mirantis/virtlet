# Apparmor profiles

In order to get the Virtlet DaemonSet work in
an [apparmor](https://gitlab.com/apparmor/apparmor/wikis/home) enabled environment follow the next steps:

* install the profiles located in this directory into the corresponding directory (/etc/apparmor.d/ if you use Debian or its derivatives)
```bash
sudo install -m 0644 libvirtd virtlet vms -t /etc/apparmor.d/
```
* apply them by
  * restarting the apparmor service
    ```bash
    sudo systemctl restart apparmor
    ```
  * or by hand, using the following commands
    ```bash
    sudo apparmor_parser -r /etc/apparmor.d/libvirtd
    sudo apparmor_parser -r /etc/apparmor.d/virtlet
    sudo apparmor_parser -r /etc/apparmor.d/vms
    ```
* set the corresponding profiles in the Virtlet DaemonSet:
```yaml
  spec:
    template:
      metadata:
        annotations:
          container.apparmor.security.beta.kubernetes.io/libvirt: localhost/libvirtd
          container.apparmor.security.beta.kubernetes.io/vms: localhost/vms
          container.apparmor.security.beta.kubernetes.io/virtlet: localhost/virtlet
```
* [re]create the Virtlet DamonSet using standard Kubernetes approach
