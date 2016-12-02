#!/bin/sh

mkdir -p opt/cni/bin
curl -L https://github.com/containernetworking/cni/releases/download/v0.3.0/cni-v0.3.0.tgz | tar zxC opt/cni/bin
