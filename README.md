# virtlet

[![Join the chat at https://gitter.im/Mirantis/virtlet](https://badges.gitter.im/Mirantis/virtlet.svg)](https://gitter.im/Mirantis/virtlet?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)

Virtlet is a Kubernetes runtime server which allows you to run VM workloads, based on QCOW2 images.

## Running local environment

To run local environment, please install [docker-compose](https://pypi.python.org/pypi/docker-compose)
at least in 1.8.0 version. If your Linux distribution is providing an older version, we suggest to
use Python virtualenv(wrapper):

```
apt-get install virtualenvwrapper
mkvirtualenv docker-compose
pip install docker-compose
```

If you have docker-compose ready to use, you can set up the virtlet dev environment by doing:

```
cd contrib/docker-compose
docker-compose up
```