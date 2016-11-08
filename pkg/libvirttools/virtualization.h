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

#ifndef PKG_LIBVIRTTOOLS_VIRTUALIZATION_H_
#define PKG_LIBVIRTTOOLS_VIRTUALIZATION_H_

#define VIRTLET_VIRTUALIZATION_ERR_BASE 2000

enum virtletVirtualizationErr {
	VIRTLET_VIRTUALIZATION_OK = 0,

	VIRTLET_VIRTUALIZATION_ERR_LIBVIRT = VIRTLET_VIRTUALIZATION_ERR_BASE + 1,
};

int defineDomain(virConnectPtr conn, char *domXML);
int createDomain(virConnectPtr conn, char *uuid);
int stopDomain(virConnectPtr conn, char *uuid);
int destroyAndUndefineDomain(virConnectPtr conn, char *uuid);

#endif  // PKG_LIBVIRTTOOLS_VIRTUALIZATION_H_
