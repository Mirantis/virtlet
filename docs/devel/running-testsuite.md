# Running tests

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

For information on how to run e2e tests, refer to [Running local environment](running-local-environment.md)

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
only run on Linux. That being said, some of the tests do run on
Mac OS X. First you need to make sure Virtlet is checked out
as `$GOPATH/src/github.com/Mirantis/virtlet` and install glide deps:

```
$ cd "$GOPATH/src/github.com/Mirantis/virtlet"
$ glide install --strip-vendor
```

```
$ go test -v ./pkg/{flexvolume,imagetranslation,libvirttools,metadata,stream,utils,tapmanager}
```
