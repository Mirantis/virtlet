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

#include <glib.h>
#include <libvirt/libvirt.h>
#include "virtualization.h"

void testDefineDomain(gconstpointer gConn) {
	virConnectPtr conn = (virConnectPtr) gConn;
	char *domXML = "<domain type='test'>"
		"    <name>test-vm</name>"
		"    <memory>2048</memory>"
		"    <uuid>e54e628a-2f8d-49c1-89b5-0b269debb9f1</uuid>"
		"    <features>"
		"        <acpi/><apic/>"
		"    </features>"
		"    <vcpu>1</vcpu>"
		"    <os>"
		"        <type>hvm</type>"
		"        <boot dev='hd'/>"
		"    </os>"
		"    <devices>"
		"        <input type='tablet' bus='usb'/>"
		"        <graphics type='vnc' port='-1'/>"
		"        <console type='pty'/>"
		"        <sound model='ac97'/>"
		"        <video>"
		"            <model type='cirrus'/>"
		"        </video>"
		"    </devices>"
		"</domain>";
	int result;

	result = defineDomain(conn, domXML);
	g_assert_cmpint(result, ==, 0);
}

void testCreateDomain(gconstpointer gConn) {
	virConnectPtr conn = (virConnectPtr) gConn;
	int result;

	result = createDomain(conn, "e54e628a-2f8d-49c1-89b5-0b269debb9f1");
	g_assert_cmpint(result, ==, 0);
}

void testStopDomain(gconstpointer gConn) {
	virConnectPtr conn = (virConnectPtr) gConn;
	int result;

	result = stopDomain(conn, "e54e628a-2f8d-49c1-89b5-0b269debb9f1");
	g_assert_cmpint(result, ==, 0);
}

void testDestroyAndUndefineDomain(gconstpointer gConn) {
	virConnectPtr conn = (virConnectPtr) gConn;
	int result;

	result = destroyAndUndefineDomain(conn,
					  "e54e628a-2f8d-49c1-89b5-0b269debb9f1");
	g_assert_cmpint(result, ==, 0);
}

int main(int argc, char **argv) {
	virConnectPtr conn;
	gconstpointer gConn;
	int result;

	if (!(conn = virConnectOpen("test:///default"))) {
		result = -1;
		goto cleanup;
	}

	gConn = (gconstpointer) conn;

	g_test_init(&argc, &argv, NULL);
	g_test_add_data_func("/defineDomain", gConn, &testDefineDomain);
	g_test_add_data_func("/createDomain", gConn, &testCreateDomain);
	g_test_add_data_func("/stopDomain", gConn, &testStopDomain);
	g_test_add_data_func("/destroyAndUndefineDomain", gConn,
			     &testDestroyAndUndefineDomain);

	result = g_test_run();

 cleanup:
	if (conn) {
		virConnectClose(conn);
	}

	return result;
}
