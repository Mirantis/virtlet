/*
Copyright 2016 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdlib.h>
#include <string.h>
#include "networking.h"

void virFree(void *ptrptr);
#define VIR_FREE(ptr) virFree(1 ? (void *) &(ptr) : (ptr))

int
hasNetwork(virConnectPtr conn, char *name)
{
	int result = 0;
	virNetworkPtr network = NULL;

	if (!(network = virNetworkLookupByName(conn, (const char*) name))) {
		result = -1;
	}

	virNetworkFree(network);

	return result;
}

int
createNetwork(virConnectPtr conn, char *xml)
{
	int result = 0;
	virNetworkPtr network = NULL;

	if (!(network = virNetworkDefineXML(conn, (const char*) xml))) {
		result = -1;
	}
	if (virNetworkSetAutostart(network, 1) < 0)
		result = -1;
	else
		result = virNetworkCreate(network);

	virNetworkFree(network);

	return result;
}

void
findIPAddress(int count, virDomainInterfacePtr *ifaces, char **ip)
{
	int i, j;
	for (i = 0; i < count; i++) {
		virDomainInterfacePtr iface = ifaces[i];
		char *ip_addr_str = NULL;

		if (!iface->naddrs) {
			continue;
		}

		for (j = 0; j < iface->naddrs; j++) {
			if (iface->addrs[j].type != VIR_IP_ADDR_TYPE_IPV4) {
				continue;
			}
			*ip = strdup(iface->addrs[j].addr);
			return;
		}
	}
	return;
}

int
getDomIfAddr(virConnectPtr conn, char *uuid, char **ip)
{
	int result = 0;
	virDomainPtr domain = NULL;
	virDomainInterfacePtr *ifaces = NULL;
	const char *ifacestr = NULL;
	int ifaces_count = 0;
	int i;

	if (!(domain = virDomainLookupByUUIDString(conn, (const char*) uuid))) {
		result = -1;
	} else {
		if ((ifaces_count = virDomainInterfaceAddresses(domain, &ifaces, VIR_DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE, 0)) < 0) {
			// TODO: set error on lack of interfaces
			result = -1;
			goto cleanup;
		}
		findIPAddress(ifaces_count, ifaces, ip);
	}

cleanup:
	if (ifaces && ifaces_count > 0) {
		for (i = 0; i < ifaces_count; i++)
			virDomainInterfaceFree(ifaces[i]);
	}
	VIR_FREE(ifaces);
	if (domain) {
		virDomainFree(domain);
	}
	return result;
}
