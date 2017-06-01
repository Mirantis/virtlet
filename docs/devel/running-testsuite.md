# Running tests

To run integration & e2e tests, please install [docker-compose](https://pypi.python.org/pypi/docker-compose)
at least in 1.8.0 version. If your Linux distribution is providing an older version, we suggest to
use [virtualenvwrapper](https://virtualenvwrapper.readthedocs.io):

```sh
apt-get install virtualenvwrapper
mkvirtualenv docker-compose
pip install docker-compose
```

In order to run the tests, use:

```sh
./test.sh
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
