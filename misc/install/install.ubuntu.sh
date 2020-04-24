#!/bin/bash

set -e


if [ ! -d "/opt/docker" ] && [ ! -d "/var/lib/docker" ]; then
  mkdir /opt/docker
  cd /var/lib
  ln -s /opt/docker .

  cat <<< 'deb http://mirrors.aliyun.com/opsx/pouch/linux/debian/ pouch stable
' > /etc/apt/sources.list.d/pouch.list

  wget http://mirrors.aliyun.com/opsx/pouch/linux/debian/opsx%40service.alibaba.com.gpg.key
  apt-key add opsx@service.alibaba.com.gpg.key

  apt update
fi

if [ ! -d "/opt/pouch" ] && [ ! -d "/var/lib/pouch" ]; then
  mkdir /opt/pouch
  cd /var/lib
  ln -s /opt/pouch .

  apt install -y docker.io pouch lxcfs psmisc rsync net-tools lvm2
  apt-get purge --auto-remove apparmor
  ## docker system prune --all --volumes
fi



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
  "lxcfs-home": "/var/lib/lxcfs",
  "lxcfs": "/usr/bin/lxcfs",
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

# SELinux is not installed by default in Ubuntu
##  if [ -e "/etc/selinux/config" ]; then
##    sed -i 's/SELINUX\=enforcing/SELINUX\=disabled/g' /etc/selinux/config
##  else
##    cat <<< 'SELINUX=disabled
##  ' > /etc/selinux/config
##  fi
##  setenforce 0

## systemctl stop apparmor
## systemctl disable apparmor

systemctl enable docker lxcfs
systemctl start docker lxcfs

exit 0
