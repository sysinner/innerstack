#!/bin/bash

set -e

cat <<< '[docker-ce-stable]
name=Docker CE Stable - $basearch
baseurl=https://mirrors.aliyun.com/docker-ce/linux/centos/7/$basearch/stable
enabled=1
gpgcheck=1
gpgkey=https://mirrors.aliyun.com/docker-ce/linux/centos/gpg

' > /etc/yum.repos.d/docker-ce.repo

cat <<< '[pouch-stable]
name=Pouch Stable - $basearch
baseurl=https://mirrors.aliyun.com/opsx/pouch/linux/centos/7/$basearch/stable
enabled=1
gpgcheck=1
gpgkey=https://mirrors.aliyun.com/opsx/pouch/linux/centos/gpg

' > /etc/yum.repos.d/pouch.repo



if [ ! -d "/opt/docker" ] && [ ! -d "/var/lib/docker" ]; then
  mkdir /opt/docker
  cd /var/lib
  ln -s /opt/docker .
fi

if [ ! -d "/opt/pouch" ] && [ ! -d "/var/lib/pouch" ]; then
  mkdir /opt/pouch
  cd /var/lib
  ln -s /opt/pouch .
fi

yum install -y docker-ce pouch psmisc rsync net-tools device-mapper-persistent-data lvm2

if [ ! -d "/etc/docker" ]; then
  mkdir /etc/docker
fi
if [ ! -f "/etc/docker/daemon.json" ]; then
  cat <<< '{
  "bip": "172.18.0.1/16",
  "registry-mirrors": ["https://registry.docker-cn.com"]
}' > /etc/docker/daemon.json
fi

if [ ! -d "/etc/pouch" ]; then
  mkdir /etc/pouch
fi
if [ ! -f "/etc/pouch/config.json" ]; then
  cat <<< '{
  "lxcfs-home": "/var/lib/pouch-lxcfs",
  "lxcfs": "/usr/local/bin/pouch-lxcfs",
  "network-config": {
    "bridge-config": {
      "bridge-name": "p0",
      "default-gateway": "172.20.0.1",
      "bip": "172.20.0.1/16",
      "iptables": true
    }
  },
  "enable-lxcfs": true
}' > /etc/pouch/config.json
fi

sed -i 's/SELINUX\=enforcing/SELINUX\=disabled/g' /etc/selinux/config
setenforce 0

systemctl enable docker pouch-lxcfs
systemctl start docker pouch-lxcfs

exit 0
