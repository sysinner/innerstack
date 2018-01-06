FROM centos:centos7
MAINTAINER Eryx <evorui#gmail.com>

LABEL "docker/image/name"="sysinner:a1el7v1"

RUN rm -f /etc/yum.repos.d/*
ADD el7.aliyun.repo /etc/yum.repos.d/base.repo

RUN yum install -y https://download.postgresql.org/pub/repos/yum/10/redhat/rhel-7-x86_64/pgdg-redhat10-10-1.noarch.rpm && \
    yum install -y sysvinit-tools psmisc rsync libaio numactl-libs openssl-libs openssh libjpeg libpng libwebp libicu libmcrypt libtool-ltdl aspell-devel libxslt postgresql10-libs openssh-server git2u && \
    yum clean all

RUN useradd -u 2048 -d /home/action action && \
    rm -f /etc/ssh/ssh_host_rsa_key /etc/ssh/ssh_host_ecdsa_key /etc/ssh/ssh_host_ed25519_key && \
    ssh-keygen -t rsa -f /etc/ssh/ssh_host_rsa_key -N '' && \
    ssh-keygen -t ecdsa -f /etc/ssh/ssh_host_ecdsa_key -N '' && \
    ssh-keygen -t ed25519 -f /etc/ssh/ssh_host_ed25519_key -N '' && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin no/g' /etc/ssh/sshd_config && \
    sed -i 's/GSSAPIAuthentication.*/GSSAPIAuthentication no/g' /etc/ssh/sshd_config && \
    chmod 644 /etc/ssh/*

ENV USER action
ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

