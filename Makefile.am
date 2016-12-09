# Copyright 2016 Mirantis
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

SUBDIRS = pkg/bolttools pkg/download pkg/libvirttools pkg/manager pkg/nettools tests/integration

# FIXME: add back dockerfilelint.test
# (as of now, it requires Docker)
check_SCRIPTS = cpplint.test gofmt.test
TESTS = $(check_SCRIPTS)

.PHONY: all vendor

all:
	if [ ! -d vendor ]; then $(MAKE) install-vendor; fi
	mkdir -p $(builddir)/_output $(builddir)/contrib/images/libvirt/_output
	go build -o $(builddir)/_output/virtlet ./cmd/virtlet
	go build -o $(builddir)/_output/vmwrapper ./cmd/vmwrapper

verify-glide-installation:
	@which glide || go get github.com/Masterminds/glide

install-vendor: verify-glide-installation
	glide install --strip-vendor

update-vendor: verify-glide-installation
	glide update --strip-vendor

clean-local:
	rm -f $(builddir)/virtlet

# need virtlet binary for integration tests
install-exec-local:
	install $(builddir)/_output/virtlet $(bindir)/virtlet
