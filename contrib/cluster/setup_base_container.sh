#!/bin/bash



set -o errexit
set -o nounset

# Install Docker engine
sudo apt-get update
sudo apt-get -y install apt-transport-https ca-certificates
sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF76221572C52609D
echo "deb https://apt.dockerproject.org/repo ubuntu-trusty main" | sudo tee /etc/apt/sources.list.d/docker.list
sudo apt-get update
sudo apt-get -y install linux-headers-$(uname -r) linux-image-extra-$(uname -r) linux-image-extra-virtual
sudo apt-get update
sudo apt-get install -y docker-engine
sudo usermod -aG docker ubuntu

# Check docker
set +e
docker ps 2> /dev/null 1> /dev/null
if [ "$?" != "0" ]; then
   echo "Failed to successfully run 'docker ps' in base container, docker wasn't installed successfully."
   exit 1
fi
set -e

sudo apt-get -y install dbus curl
sudo curl -o /usr/local/bin/docker-compose -L "https://github.com/docker/compose/releases/download/1.8.1/docker-compose-$(uname -s)-$(uname -m)"
sudo chmod +x /usr/local/bin/docker-compose
docker-compose -v 2> /dev/null 1> /dev/null
if [ "$?" != "0" ]; then
   echo "Failed to successfully run 'docker-compose -v' in base container, docker-compose wasn't installed successfully."
   exit 1
fi

sudo chown root:root /dev/kvm
ln -s /etc/apparmor.d/docker /etc/apparmor.d/force-complain/docker
