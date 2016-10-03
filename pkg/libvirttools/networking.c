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
#include "networking.h"

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
