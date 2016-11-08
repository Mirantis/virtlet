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

#ifndef PKG_LIBVIRTTOOLS_IMAGE_H_
#define PKG_LIBVIRTTOOLS_IMAGE_H_

#include <stdbool.h>

#define VIRTLET_IMAGE_ERR_BASE 1000

enum virtletImageErr {
	VIRTLET_IMAGE_OK = 0,

	VIRTLET_IMAGE_ERR_SEND_STREAM = VIRTLET_IMAGE_ERR_BASE + 1,
	VIRTLET_IMAGE_ERR_ALREADY_EXISTS = VIRTLET_IMAGE_ERR_BASE + 2,
	VIRTLET_IMAGE_ERR_LIBVIRT = VIRTLET_IMAGE_ERR_BASE + 3,
};

int virtletVolUploadSource(virStreamPtr stream, char *bytes, size_t nbytes,
			   void *opaque);
int pullImage(virConnectPtr conn, virStoragePoolPtr pool, char *shortName,
	      char *filepath, char *volXML);

#endif  // PKG_LIBVIRTTOOLS_IMAGE_H_
