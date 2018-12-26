# Running unit and integration tests

In order to run unit tests, use:

```
$ build/cmd.sh test
```

In order to run integration, use:

```
$ build/cmd.sh integration
```

There's a number of 'golden master' tests in Virtlet. These tests work
by generating some JSON data and comparing it to the previous version
of that data stored in git index (i.e. staged or committed). The test
fails if there's any difference, and the file is updated in this case.
You need to stage or commit the changes to make the test pass (or fix
the code so there's no changes).

Sample usage:

```
$ cd pkg/libvirttools/

$ ../../build/cmd.sh gotest
--- FAIL: TestDomainDefinitions (0.35s)
    --- FAIL: TestDomainDefinitions/cloud-init_with_user_data (0.05s)
        gm.go:58: got difference for "TestDomainDefinitions/cloud-init_with_user_data":
                diff --git a/pkg/libvirttools/TestDomainDefinitions__cloud-init_with_user_data.json b/pkg/libvirttools/TestDomainDefinitions__cloud-init_with_user_data.json
                index e852f8b..6db03cc 100755
                --- a/pkg/libvirttools/TestDomainDefinitions__cloud-init_with_user_data.json
                +++ b/pkg/libvirttools/TestDomainDefinitions__cloud-init_with_user_data.json
                @@ -66,7 +66,7 @@
                     "name": "domain conn: iso image",
                     "data": {
                       "meta-data": "{\"instance-id\":\"testName_0.default\",\"local-hostname\":\"testName_0\",\"public-keys\":[\"key1\",\"key2\"]}",
                -      "user-data": "#cloud-config\nusers:\n- name: cloudy\n"
                +      "user-data": "#cloud-config\nusers:\n- name: cloudy1\n"
                     }
                   },
                   {
FAIL
exit status 1
FAIL    github.com/Mirantis/virtlet/pkg/libvirttools    0.466s

$ # accept the changes by staging them
$ git add TestDomainDefinitions__cloud-init_with_user_data.json

$ ../../build/cmd.sh gotest
PASS
ok      github.com/Mirantis/virtlet/pkg/libvirttools    0.456s
```

# Running tests on Mac OS X

To run tests on Mac OS X you need a working Go 1.8 installation and
[Glide](https://glide.sh/). You also need to install `cdrtools`
package and then make a symbolic link for `mkisofs` named
`genisoimage`:

```
$ brew install cdrtools
$ sudo ln -s `which mkisofs` /usr/local/bin/genisoimage
```

Some of the tests such as integration/e2e and network related tests
only run on Linux. That being said, some of the tests do run on Mac OS
X. First you need to make sure Virtlet is checked out as
`$GOPATH/src/github.com/Mirantis/virtlet` and install the glide deps:

```
$ cd "$GOPATH/src/github.com/Mirantis/virtlet"
$ glide install --strip-vendor
```

```
$ go test -v ./pkg/{flexvolume,imagetranslation,libvirttools,metadata,stream,utils,tapmanager}
```

# Running e2e tests

You need a running Kubernetes cluster to run e2e tests.  Virtlet e2e
tests can be run using Virtlet build container:

```
$ # run some e2e tests
$ build/cmd.sh e2e -test.v

$ # run e2e tests that have 'Should have default route' in their description
$ build/cmd.sh e2e -test.v -ginkgo.focus="Should have default route"
```

You can also build the e2e runner locally and run it on either Mac or
Linux:
```
$ glide i --strip-vendor
$ go test -i -c -o _output/virtlet-e2e-tests ./tests/e2e
$ _output/virtlet-e2e-tests -ginkgo.v
```

Virtlet uses [Ginkgo](https://onsi.github.io/ginkgo/) for its e2e
tests, so you can refer to the list of
[Ginkgo CLI flags](https://onsi.github.io/ginkgo/#the-ginkgo-cli). The
Ginkgo flags should be passed using `-ginkgo.XXX` convention.

The following additional flags are recognized by Virtlet e2e runner:

* `-cluster-url=URL` (default `http://127.0.0.1:8080`) specifies an
  insecure apiserver endpoint to use. You can use `kubectl proxy` when
  running the tests against a real cluster.
* `-image=LOCATION` (defaults to Virtlet's modified CirrOS image)
  specifies the location of the image to use for the tests. The
  `LOCATION` is an URL without the protocol prefix (`http(s)://`).
* `-sshuser=USER` specifies ssh user name that should be used with the
  image.
* `-include-cloud-init-tests=` (defaults to false) specifies that
  the cloud-init tests should included.
* `-memoryLimit=N` (defaults to 160) specifies the memory limit for
  the VM that should be used. You may need to adjust this setting
  according to the requirements of the image.
* `-include-unsafe-tests` (defaults to false) includes the tests that
  can be unsafe if they're run outside the build container.
  **Use with care!**
* `-junitOutput=FILENAME` specifies that a JUnit XML output file
  should be produced by the runner.

Tests may have several labels which are included in their names and
can be used in `-ginkgo.focus` / `ginkgo.skip` options:

* `[Conformance]` specifies the tests that any properly configured
  Kubernetes cluster with Virtlet should pass.
* `[Heavy]` specifies the tests that shouldn't be run when KVM cannot
  be used, for example, on Virtlet public CI.
* `[Disruptive]` tests may break your cluster or cause some or all of
  the VMs to be restarted.
* `[MultiCNI]` tests should be used to examine a multi-CNI setup.
* `[Flaky]` tests contain flakes that should be fixed. This label
  basically designates a known bug in either the test or Virtlet
  itself. These tests are skipped on Virtlet public CI.
