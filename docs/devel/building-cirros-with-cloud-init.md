# Building own CirrOS image

The purpose of this document is to provide instructions for building own CirrOS
image with installed in rootfs/init sequence python based [Cloud Init](http://cloudinit.readthedocs.io/en/latest/).

## Preparation

In this example we are basing on official sources from [launchpad](https://git.launchpad.net/cirros)
Assuming that you have already installed git, clone above repository with command:

```sh
git clone https://git.launchpad.net/cirros
```

The assumption is that rest of the process is done in the `cirros` directory, so...

```sh
cd cirros
patch -p1 </path/to/virtlet-repository/contrib/cirros-patches/cirros-repo.diff
```

Prepre `downloads` directory and put into it buildroot tar:

```sh
mkdir ../downloads ; ln -s ../downloads
br_ver="2017.02"
( cd downloads ; wget http://buildroot.uclibc.org/downloads/buildroot-${br_ver}.tar.gz )
tar xf buildroot-${br_ver}.tar.gz
```

Ensure that you have installed packages listed in `cirros/bin/system-setup`
(names of packages can be different for different systems, for debian based
system you can simply use mentioned tool to install them).

## Patches applying

```sh
( cd buildroot && QUILT_PATCHES=$PWD/../patches-buildroot quilt push -a )
( cd buildroot && patch -p1 </path/to/virtlet-repository/contrib/cirros-patches/buildroot.diff )
```

## Retrieving sources of buildroot packages

```sh
make ARCH=i386 br-source
```

## Building buildroot

```sh
make ARCH=i386 OUT_D=$PWD/output/i386
```

The process should fail during installation of `python-cloud-init`. After this
failure, use below command:

```sh
( cd output/i386/buildroot ; cp -a build/python-cloud-init*/build/lib/cloudinit target/usr/lib/python2.7/site-packages )
sed -i -e 's/BR2_PACKAGE_PYTHON_CLOUD_INIT=y/# BR2_PACKAGE_PYTHON_CLOUD_INIT is not set/' conf/buildroot-i386.config
```

and run again previous build command:

```sh
make ARCH=i386 OUT_D=$PWD/output/i386
```

## Downloading kernel and grub

You need to set correct version of kernel (look for them on
[this](https://launchpad.net/ubuntu/+source/linux) site) eg.:

```sh
kver="3.19.0-84.92"
./bin/grab-kernels "$kver"
```

Same for grub, looking for [this](https://launchpad.net/ubuntu/+source/grub2) site:

```sh
gver="2.02~beta3-4ubuntu2.2"
./bin/grab-grub-efi "$gver"
```

## Building final image

```sh
sudo ./bin/bundle -v --arch=i386 output/i386/rootfs.tar \
      download/kernel-i386.deb download/grub-efi-i386.tar.gz output/i386/images
```

Final image will be available in file with `output/i386/images/disk.img` name.
