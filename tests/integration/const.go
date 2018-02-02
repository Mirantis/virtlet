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

package integration

var (
	imageCirrosUrl     = "localhost/cirros.img"
	imageCirrosRef     = "localhost/cirros.img@sha256:fcd9e9a622835de8dba6b546481d13599b1e592bba1275219e1b31cae33b1365"
	imageCopyCirrosUrl = "localhost/copy/cirros.img"
	imageCopyCirrosRef = "localhost/copy/cirros.img@sha256:fcd9e9a622835de8dba6b546481d13599b1e592bba1275219e1b31cae33b1365"
	imageCirrosSha256  = "fcd9e9a622835de8dba6b546481d13599b1e592bba1275219e1b31cae33b1365"
	imageCirrosId      = "sha256:" + imageCirrosSha256
	cirrosImageSize    = 23327232
)
