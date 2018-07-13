======================
Apparmor profiles
======================

In order to get virtlet works on an apparmor enabled environment
you need to install these profiles into /etc/apparmor.d/
and apply them by restarting the apparmor service or by hand, using
the following commands:

.. code-block:: bash

  $ apparmor_parser -r /etc/apparmor.d/libvirtd
  $ apparmor_parser -r /etc/apparmor.d/virtlet
  $ apparmor_parser -r /etc/apparmor.d/vms


After that set the corresponding profiles in your daemonset:

.. code-block:: yaml

  spec:
    template:
      metadata:
        annotations:
          container.apparmor.security.beta.kubernetes.io/libvirt: localhost/libvirtd
          container.apparmor.security.beta.kubernetes.io/vms: localhost/vms
          container.apparmor.security.beta.kubernetes.io/virtlet: localhost/virtlet


And [re-]create the damonset using standard kubernetes approach.
