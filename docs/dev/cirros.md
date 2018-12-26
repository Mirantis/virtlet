# Building own CirrOS image

The purpose of this document is to provide instructions for building
custom CirrOS image that has proper Python-based [Cloud Init](http://cloudinit.readthedocs.io/en/latest/)
implementation installed and added to the init sequence.


## Preparation

This example is based on the official sources hosted on [launchpad](https://git.launchpad.net/cirros).
Assuming that you have already installed git, clone the above repository with
the following command:

```sh
git clone https://git.launchpad.net/cirros
```

Let's go to the `cirros/`. The assumption is that the rest of the
commands are run in that directory.

```sh
cd cirros
patch -p1 </path/to/virtlet-repository/contrib/cirros-patches/cirros-repo.diff
```

Create the `downloads` directory and download the buildroot tar into it:

```sh
mkdir ../downloads ; ln -s ../downloads
br_ver="2017.02"
( cd downloads ; wget http://buildroot.uclibc.org/downloads/buildroot-${br_ver}.tar.gz )
tar xf buildroot-${br_ver}.tar.gz
```

Ensure that you have installed the packages listed in `cirros/bin/system-setup`
(package names can be different for different Linux distros, for Debian-based
system you can simply use `cirros/bin/system-setup` to install them).

## Applying the patches

```sh
( cd buildroot && QUILT_PATCHES=$PWD/../patches-buildroot quilt push -a )
( cd buildroot && patch -p1 </path/to/virtlet-repository/contrib/cirros-patches/buildroot.diff )
```

## Retrieving the sources of buildroot packages

```sh
make ARCH=i386 br-source
```

## Building buildroot

The initial `make` command is expected to fail.

```sh
make ARCH=i386 OUT_D=$PWD/output/i386
```

Use the following command after `make` fails:

```sh
( cd output/i386/buildroot ; cp -a build/python-cloud-init*/build/lib/cloudinit target/usr/lib/python2.7/site-packages )
sed -i -e 's/BR2_PACKAGE_PYTHON_CLOUD_INIT=y/# BR2_PACKAGE_PYTHON_CLOUD_INIT is not set/' conf/buildroot-i386.config
```

then run the previous build command again:

```sh
make ARCH=i386 OUT_D=$PWD/output/i386
```

## Downloading kernel and GRUB

You need to set a correct version of kernel (the versions are listed
[here](https://launchpad.net/ubuntu/+source/linux)) e.g.:

```sh
kver="4.13.0-32.35"
./bin/grab-kernels "$kver"
```

Same for GRUB (version list is [here](https://launchpad.net/ubuntu/+source/grub2)):

```sh
gver="2.02~beta3-4ubuntu2.2"
./bin/grab-grub-efi "$gver"
```

## Building the final image

```sh
sudo ./bin/bundle -v --arch=i386 output/i386/rootfs.tar \
      download/kernel-i386.deb download/grub-efi-i386.tar.gz output/i386/images
```

The final image will be named `output/i386/images/disk.img`.
