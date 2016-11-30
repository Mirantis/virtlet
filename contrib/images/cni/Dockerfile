FROM busybox

Maintainer Piotr Skamruk <pskamruk@mirantis.com>

ADD etc/ /
ADD opt/ /
ADD start.sh /

VOLUME /etc/cni
VOLUME /opt/cni

CMD ["/start.sh"]
