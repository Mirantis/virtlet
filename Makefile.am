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
export CGO_LDFLAGS=$(LDFLAGS)

SUBDIRS = pkg/download pkg/etcdtools pkg/libvirttools pkg/manager pkg/sandbox

.PHONY: all vendor

all:
	go build -o $(builddir)/virtlet ./cmd/virtlet
	go build -o $(builddir)/virtlet-fake-image-pull ./cmd/fake-image-pull

vendor:
	glide update --strip-vcs --strip-vendor --update-vendored --delete
	glide-vc --only-code --no-tests --keep="**/*.json.in"

clean-local:
	rm -f $(builddir)/virtlet $(builddir)/virtlet-fake-image-pull

install-exec-local:
	install $(builddir)/virtlet $(bindir)/virtlet
	install $(builddir)/virtlet-fake-image-pull $(bindir)/virtlet-fake-image-pull
