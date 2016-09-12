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
#include "virtualization.h"

int
defineAndCreateDomain(virConnectPtr conn, char *domXML)
{
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainDefineXML(conn, (const char*) domXML)) ||
	    virDomainCreate(domain) < 0) {
		result = -1;
	}

	virDomainFree(domain);

	return result;
}

int
createDomain(virConnectPtr conn, char *name)
{
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainLookupByName(conn, (const char*) name)) ||
	    virDomainCreate(domain) < 0) {
		result = -1;
	}

	virDomainFree(domain);

	return result;
}

int
stopDomain(virConnectPtr conn, char *name)
{
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainLookupByName(conn, (const char*) name)) ||
	    virDomainShutdown(domain) < 0) {
		result = -1;
	}

	virDomainFree(domain);

	return result;
}

int
destroyAndUndefineDomain(virConnectPtr conn, char *name)
{
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainLookupByName(conn, (const char*) name)) ||
	    virDomainDestroy(domain) < 0 ||
	    virDomainUndefine(domain) < 0) {
		result = -1;
	}

	virDomainFree(domain);
}
