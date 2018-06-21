/*
Copyright 2018 Mirantis

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

// This package helps to deal with switching to other process
// namespaces to execute some particular piece of code.  While
// starting from Go 1.10 it's possible to switch to different non-mnt
// namespaces without the danger of corrupting other goroutines'
// state, there's still a problem of not being able to switch to
// another mount namespace from a Go program without the "constructor"
// hack. For more info, see
// https://stackoverflow.com/a/25707007/40846
// https://github.com/golang/go/issues/8676
package nsfix
