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

#include <errno.h>
#include <fcntl.h>
#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include "image.h"

int
virtletVolUploadSource(virStreamPtr stream, char *bytes, size_t nbytes,
                       void *opaque)
{
	if (opaque == NULL) {
		return -1;
	}

	int *fd = opaque;
	return read(*fd, bytes, nbytes);
}

int
pullImage(virConnectPtr conn, virStoragePoolPtr pool, char *shortName, char *filepath,
	  char *volXML)
{
	int result = 0;
	virStorageVolPtr vol = NULL;
	int fd = -1;
	virStreamPtr stream = NULL;

	if ((fd = open(filepath, O_RDONLY)) < 0) {
		result = errno;
		goto cleanup;
	}

	if (!(vol = virStorageVolCreateXML(pool, (const char*) volXML, 0)) ||
	    !(stream = virStreamNew(conn, 0)) ||
	    virStorageVolUpload(vol, stream, 0, 0, 0) < 0 ||
	    virStreamSendAll(stream, virtletVolUploadSource, &fd) < 0 ||
	    virStreamFinish(stream) < 0) {
		result = -1;
	}

 cleanup:
	if (vol) {
		virStorageVolFree(vol);
	}
	if (fd) {
		close(fd);
	}
	if (stream) {
		virStreamFree(stream);
	}
	return result;
}
