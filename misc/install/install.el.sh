#!/bin/bash

set -e

cat <<< '[docker-ce-stable]
name=Docker CE Stable - $basearch
baseurl=https://mirrors.aliyun.com/docker-ce/linux/centos/$releasever/$basearch/stable
enabled=1
gpgcheck=1
gpgkey=https://mirrors.aliyun.com/docker-ce/linux/centos/gpg

[sysinner]
name=SysInner - InnerStack beta
baseurl=https://www.sysinner.com/repo/el/$releasever/x86_64/beta
enabled=1
gpgcheck=0

' > /etc/yum.repos.d/sysinner.repo


yum install -y sysinner-innerstack

systemctl enable innerstack
systemctl start innerstack

exit 0
