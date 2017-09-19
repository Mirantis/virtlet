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

`build`, `test`, `integration`, `run`, `gobuild`, `gotest`, `copy`,
`copy-back` and `prepare-vendor` commands check whether the build
container image and data volumes are available before proceeding. They
build the image if necessary and create the data volumes if they don't
exist, then, on each run they copy the local sources into
`virtlet_src` data volume.

The build container used by `build/cmd.sh` needs to be able to access
Docker socket inside it. By default it mounts `/var/run/docker.sock`
inside the container but you can override the path by setting
`DOCKER_SOCKET_PATH` environment variable.

## build

Performs a full build of Virtlet. Also builds
`mirantis/virtlet:latest` image.

## test

Runs the unit tests inside a build container.

## integration

Runs the integration tests. KVM is disabled for these so as to make them run
inside limited CI environments that don't support kernel virtualization.
You need to invoke `build/cmd.sh build` before running this command.

## copy

Extracts output binaries from build container into `_output/` in the
current directory.

## copy-back

Syncs local `_output_` contents to the build container. This is used
by CI for passing build artifacts between the jobs in a workflow.

## copy-dind

Copies the binaries into kube-node-1 of `kubeadm-dind-cluster` (or
kube-master if `VIRTLET_ON_MASTER` environment variable is set to a
non-empty value). You need to do `dind-cluster...sh up` to be able to
use this command.

## start-dind

Starts Virtlet on kube-node-1 of `kubeadm-dind-cluster` (or
kube-master if `VIRTLET_ON_MASTER` environment variable is set to a
non-empty value). You need to do `dind-cluster...sh up` and
`build/cmd.sh copy-dind` to be able to use this command.
This command copies locally-built `mirantis/virtlet` image to
the DIND node that will run Virtlet if it doesn't exist there
or if `FORCE_UPDATE_IMAGE` is set to a non-empty value.

## vsh

Starts an interactive shell using build container. Useful for debugging.

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

## prepare-vendor

Populates `vendor/` directory inside the build container. This is used
by CI.

## start-build-container

Starts the build container. This is done automatically by other
`build/cmd.sh` commands that need the build container.
