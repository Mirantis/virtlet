# virtlet pod examples

To be able to use the following pod definitions, please fetch the images and serve
them via HTTP server. This can be done like this:

```sh
wget http://download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img -O cirros
wget http://ftp.ps.pl/pub/Linux/fedora-linux/releases/24/CloudImages/x86_64/images/Fedora-Cloud-Base-24-1.2.x86_64.qcow2 -O fedora
sudo twistd -n web --path . -p 80
```

Then you will be able to create this pods in your local k8s cluster:

```sh
./cluster/kubectl.sh $GOPATH/src/github.com/Mirantis/virtlet/examples/virt-cirros.yaml
./cluster/kubectl.sh $GOPATH/src/github.com/Mirantis/virtlet/examples/virt-fedora.yaml
```
