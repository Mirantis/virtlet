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

import libvirt


def main():
    conn = libvirt.open("qemu:///system")
    domains = conn.listAllDomains()

    print("Cleaning up VMs")

    for domain in domains:
        domain_name = domain.name()

        print("Destroying VM", domain_name)
        if domain.destroy() < 0:
            sys.stderr.write("Failed to destroy VM %s\n" % domain_name)
            sys.exit(1)

        print("Undefining VM", domain_name)
        if domain.undefine() < 0:
            sys.stderr.write("Failed to undefine VM %s\n" % domain_name)
            sys.exit(1)

    print("All VMs cleaned")

    pool = conn.storagePoolLookupByName("default")
    volumes = pool.listAllVolumes()

    print("Cleaning up volumes")

    for volume in volumes:
        volume_name = volume.name()

        print("Deleting volume", volume_name)
        if volume.delete() < 0:
            sys.stderr.write("Failed to remove volume %s\n" % volume_name)
            sys.exit(1)

    print("All volumes cleaned")


if __name__ == "__main__":
    main()
