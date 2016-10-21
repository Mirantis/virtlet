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

#ifndef PKG_LIBVIRTTOOLS_ALLOC_UTIL_H_
#define PKG_LIBVIRTTOOLS_ALLOC_UTIL_H_

#include <libvirt/libvirt.h>
#include <unistd.h>

#define _cleanup_(x) __attribute__((cleanup(x)))

#define DEFINE_CLEANUP_FUNC(name, type, func) \
	static inline void name(type *p) {    \
		if (*p) {                     \
			func(*p);             \
		}                             \
	}

DEFINE_CLEANUP_FUNC(cleanupFd, int, close);
DEFINE_CLEANUP_FUNC(cleanupVirConnect, virConnectPtr, virConnectClose);
DEFINE_CLEANUP_FUNC(cleanupVirDomain, virDomainPtr, virDomainFree);
DEFINE_CLEANUP_FUNC(cleanupVirStorageVol, virStorageVolPtr, virStorageVolFree);
DEFINE_CLEANUP_FUNC(cleanupVirStream, virStreamPtr, virStreamFree);

#define DEFINE_VARIABLE_WITH_AUTOCLEANUP(type, func, name, value) \
	type name _cleanup_(func) = value
#define DEFINE_FD(name) \
	DEFINE_VARIABLE_WITH_AUTOCLEANUP(int, cleanupFd, name, -1)
#define DEFINE_VIR_CONNECT(name) \
	DEFINE_VARIABLE_WITH_AUTOCLEANUP(virConnectPtr, cleanupVirConnect, name, NULL)
#define DEFINE_VIR_DOMAIN(name) \
	DEFINE_VARIABLE_WITH_AUTOCLEANUP(virDomainPtr, cleanupVirDomain, name, NULL)
#define DEFINE_VIR_STORAGE_VOL(name) \
	DEFINE_VARIABLE_WITH_AUTOCLEANUP\
	(virStorageVolPtr, cleanupVirStorageVol, name, NULL)
#define DEFINE_VIR_STREAM(name) \
	DEFINE_VARIABLE_WITH_AUTOCLEANUP(virStreamPtr, cleanupVirStream, name, NULL)

#endif  // PKG_LIBVIRTTOOLS_ALLOC_UTIL_H_
