# build/cmd.sh usage

In general, `build/cmd.sh` script help automating usage of docker build
container based on [Dockerfile.build](../../Dockerfile.build).

Currently it has this list of commands:
 * `./build/cmd.sh build`
 * `./build/cmd.sh test`
 * `./build/cmd.sh copy`
 * `./build/cmd.sh stop`
 * `./build/cmd.sh clean`
 * `./build/cmd.sh gotest [TEST_ARGS...]`
 * `./build/cmd.sh gobuild [BUILD_ARGS...]`
 * `./build/cmd.sh run CMD...`

## build

Performs full build based on autotools.

## test

Runs tests on libvirt/qemu with KVM disabled - suitable for CI on platforms
(like Travis) not supporting kernel virtualization.

## copy

Extracts output binaries from build container into `_output/` in current
directory.

## stop

Removes build container.

## clean

Removes build container, it's image, it's volumes used during build process
as also output binaries.

## gotest

Runs unit tests suite in build container - suitable for connecting with IDE.

## gobuild

Runs build process (assumes that there was called `build` command at last once
from last time of change autotools configuration) of sources in build
container - suitable as syntax check for IDE integration.

## run

Runs `CMD` inside of build container, suitable for checking it's internals
during debug of build process session.
