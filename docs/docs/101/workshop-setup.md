# Workshop setup
## Clone repository

In your shell clone the Virtlet repository and download `virtletctl` binary:

```bash
git clone https://github.com/Mirantis/virtlet.git 
chmod 600 virtlet/examples/vmkey

wget https://github.com/Mirantis/virtlet/releases/download/v1.4.4/virtletctl
chmod +x virtletctl

wget https://storage.googleapis.com/kubernetes-release/release/v1.14.1/bin/linux/amd64/kubectl
chmod +x kubectl

mkdir -p ~/bin
mv virtletctl ~/bin
mv kubectl ~/bin

cd virtlet
```
