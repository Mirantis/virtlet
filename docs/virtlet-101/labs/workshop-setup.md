# Workshop setup
## Clone repository

In your shell clone the Virtlet repository and download `virtletctl` binary:

```bash
git clone https://github.com/Mirantis/virtlet.git 
chmod 600 virtlet/examples/vmkey

wget https://github.com/Mirantis/virtlet/releases/download/v1.4.1/virtletctl
chmod +x virtletctl

wget https://storage.googleapis.com/kubernetes-release/release/v1.11.0/bin/linux/amd64/kubectl
chmod +x kubectl

mkdir -p ~/bin
mv virtletctl ~/bin
mv kubectl ~/bin

cd virtlet
```

Next [Provision Virtlet](provision-virtlet.md)
