#!/usr/bin/env python

# Copyright 2016 Mirantis
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import print_function
import sys
import traceback

import libvirt


def destroy_domain(domain):
    if domain.state() != libvirt.VIR_DOMAIN_RUNNING:
        return

    try:
        domain.destroy()
    except libvirt.libvirtError:
        sys.stderr.write("Failed to destroy VM %s\n" % domain.name())
        traceback.print_exc(file=sys.stderr)
        sys.exit(1)


def undefine_domain(domain):
    try:
        domain.undefine()
    except libvirt.libvirtError:
        sys.stderr.write("Failed to undefine VM %s\n" % domain.name())
        traceback.print_exc(file=sys.stderr)
        sys.exit(1)


def cleanup_volumes(conn):
    try:
        pool = conn.storagePoolLookupByName("default")
    except libvirt.libvirtError:
        return

    volumes = pool.listAllVolumes()

    print("Cleaning up volumes")

    for volume in volumes:
        volume_name = volume.name()

        print("Deleting volume", volume_name)
        if volume.delete() < 0:
            sys.stderr.write("Failed to remove volume %s\n" % volume_name)
            sys.exit(1)

    print("All volumes cleaned")


def main():
    conn = libvirt.open("qemu:///system")
    domains = conn.listAllDomains()

    print("Cleaning up VMs")

    for domain in domains:
        print("Destroying VM", domain.name())
        destroy_domain(domain)

        print("Undefining VM", domain.name())
        undefine_domain(domain)

    print("All VMs cleaned")

    cleanup_volumes(conn)

    conn.close()


if __name__ == "__main__":
    main()
