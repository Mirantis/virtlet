# Copyright 2017 Mirantis
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

export GO15VENDOREXPERIMENT=1
export CGO_CFLAGS=$(CFLAGS)
export CGO_LDFLAGS=$(LDFLAGS) $(LIBS)

SUBDIRS = pkg/metadata pkg/libvirttools pkg/manager pkg/nettools pkg/criproxy tests/integration tests/network pkg/log pkg/flexvolume

# FIXME: add back dockerfilelint.test
# (as of now, it requires Docker)
check_SCRIPTS = gofmt.test
TESTS = $(check_SCRIPTS)

.PHONY: all vendor

all:
	if [ ! -d vendor ]; then \
		$(MAKE) install-vendor; \
	fi
	mkdir -p $(builddir)/_output $(builddir)/contrib/images/libvirt/_output
	go build -i -o $(builddir)/_output/virtlet ./cmd/virtlet
	go build -i -o $(builddir)/_output/vmwrapper ./cmd/vmwrapper
	go build -i -o $(builddir)/_output/criproxy ./cmd/criproxy
	go build -i -o $(builddir)/_output/flexvolume_driver ./cmd/flexvolume_driver
	go build -i -o $(builddir)/_output/virtlet_log ./cmd/virtlet_log
	go test -i -c -o $(builddir)/_output/virtlet-e2e-tests ./tests/e2e

verify-glide-installation:
	@which glide || go get github.com/Masterminds/glide

install-vendor: verify-glide-installation
	glide install --strip-vendor
	if [ ! -f ./vendor/github.com/libvirt/libvirt-go-xml/domain.go.orig ]; then \
		patch -d vendor/github.com/libvirt/libvirt-go-xml -p1 -b -i $(CURDIR)/libvirt-xml-go.patch; \
	fi

update-vendor: verify-glide-installation
	glide update --strip-vendor

clean-local:
	rm -f $(builddir)/virtlet

# need virtlet binary for integration tests
install-exec-local:
	install $(builddir)/_output/virtlet $(bindir)/virtlet
