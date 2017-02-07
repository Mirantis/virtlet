# virtlet pod examples

To be able to use the following pod definitions, please fetch the images and serve
them via HTTP server. Example how to do this:

```sh
wget http://download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img -O cirros
wget http://ftp.ps.pl/pub/Linux/fedora-linux/releases/24/CloudImages/x86_64/images/Fedora-Cloud-Base-24-1.2.x86_64.qcow2 -O fedora
sudo python2 -m SimpleHTTPServer 80
```

YAML files from these directory contain an IP address, which should be an address of
host which is avaliable from the container - so the host's address in docker0 network.
You can get this address by the following command:

```
docker exec dockercompose_virtlet_1 ip route | head -1 | awk '{print $3;}'
```

If it differs from what the example YAML files contain, please edit them.

Then you will be able to create this pods in your local k8s cluster:

```sh
./cluster/kubectl.sh create -f $GOPATH/src/github.com/Mirantis/virtlet/examples/virt-cirros.yaml
./cluster/kubectl.sh create -f $GOPATH/src/github.com/Mirantis/virtlet/examples/virt-fedora.yaml
```
