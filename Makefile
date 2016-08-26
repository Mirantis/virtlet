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

BUILD_DIR ?= ./out
PREFIX ?= /usr/local
bindir = $(PREFIX)/bin

.PHONY: all install vendor clean

all:
	go build -o $(BUILD_DIR)/virtlet ./cmd/virtlet
	go build -o $(BUILD_DIR)/virtlet-fake-image-pull ./cmd/fake-image-pull

install:
	install $(BUILD_DIR)/virtlet $(bindir)/virtlet
	install $(BUILD_DIR)/virtlet-fake-image-pull $(bindir)/virtlet-fake-image-pull

vendor:
	glide update --strip-vcs --strip-vendor --update-vendored --delete
	glide-vc --only-code --no-tests --keep="**/*.json.in"

clean:
	rm -rf $(BUILD_DIR)
