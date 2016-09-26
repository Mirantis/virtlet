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

check_PROGRAMS = image.test virtualization.test
check_SCRIPTS = go.test
TESTS = $(check_PROGRAMS) $(check_SCRIPTS)

image_test_CFLAGS = $(CFLAGS) -I.
image_test_SOURCES = tests/test_image.c image.h image.c

virtualization_test_CFLAGS = $(CFLAGS) -I.
virtualization_test_SOURCES = tests/test_virtualization.c virtualization.h virtualization.c
