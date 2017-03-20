# build/cmd.sh usage

`build/cmd.sh` script helps automating usage of docker build
container using [Dockerfile.build](../../Dockerfile.build).

Currently the script supports the following commands:
 * `./build/cmd.sh build`
 * `./build/cmd.sh test`
 * `./build/cmd.sh copy`
 * `./build/cmd.sh stop`
 * `./build/cmd.sh clean`
 * `./build/cmd.sh gotest [TEST_ARGS...]`
 * `./build/cmd.sh gobuild [BUILD_ARGS...]`
 * `./build/cmd.sh run CMD...`

`build`, `test`, `run`, `gobuild`, `gotest` commands check whether
the build container image and data volumes are available
before proceeding. They build the image if necessary and create
the data volumes if they don't exist, then, on each run they copy the
local sources into `virtlet_src` data volume.

## build

Performs full build based on autotools.

## test

Runs tests inside a build container on libvirt/qemu with KVM disabled - suitable
for CI on platforms not supporting kernel virtualization, like Travis.
Tests run is preceded by full build based on autotools.

## copy

Extracts output binaries from build container into `_output/` in current
directory.

## stop

Removes the build container.

## clean

Removes the build container and image along with data volumes as well as
the binaries in local `_output` directory.

## gotest

Runs unit tests suite in build container - suitable for use with IDEs/editors.

## gobuild

Runs go build in the build container. This command can be used for
syntax checking in IDEs/editors. It assumes that `build` command was invoked
at least once since the last `clean`.

## run

Runs the specified command inside the build container. This command can be
used to debug the build scripts.
