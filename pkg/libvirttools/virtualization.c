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

int defineDomain(virConnectPtr conn, char *domXML) {
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainDefineXML(conn, (const char*) domXML))) {
		result = -1;
	}

	if (domain) {
		virDomainFree(domain);
	}

	return result;
}

int createDomain(virConnectPtr conn, char *uuid) {
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainLookupByUUIDString(conn, (const char*) uuid)) ||
	    virDomainCreate(domain) < 0) {
		result = -1;
	}

	if (domain) {
		virDomainFree(domain);
	}

	return result;
}

int stopDomain(virConnectPtr conn, char *uuid) {
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainLookupByUUIDString(conn, (const char*) uuid)) ||
	    virDomainShutdown(domain) < 0) {
		result = -1;
	}

	if (domain) {
		virDomainFree(domain);
	}

	return result;
}

int destroyAndUndefineDomain(virConnectPtr conn, char *uuid) {
	int result = 0;
	virDomainPtr domain = NULL;

	if (!(domain = virDomainLookupByUUIDString(conn, (const char*) uuid)) ||
	    virDomainDestroy(domain) < 0 ||
	    virDomainUndefine(domain) < 0) {
		result = -1;
	}

	if (domain) {
		virDomainFree(domain);
	}
}
