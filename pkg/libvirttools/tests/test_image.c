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

#include <fcntl.h>
#include <glib.h>
#include <libvirt/libvirt.h>
#include "image.h"

void testVirtletVolUploadSourceNullOpaque()
{
	virConnectPtr conn;
	virStreamPtr stream;
	int result;

	if (!(conn = virConnectOpen("test:///default")) ||
	    !(stream = virStreamNew(conn, 0))) {
		g_test_fail();
		goto cleanup;
	}

	result = virtletVolUploadSource(stream, "", 0, NULL);
	g_assert_cmpint(result, ==, -1);

 cleanup:
	if (stream) {
		virStreamFree(stream);
	}
	if (conn) {
		virConnectClose(conn);
	}
}

int
main(int argc, char **argv)
{
	g_test_init(&argc, &argv, NULL);
	g_test_add_func("/virtletVolUploadSourceNullOpaque", &testVirtletVolUploadSourceNullOpaque);

	return g_test_run();
}
