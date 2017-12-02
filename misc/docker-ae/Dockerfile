FROM centos:centos7
MAINTAINER Eryx <evorui#gmail.com>

LABEL "docker/image/name"="sysinner:a1el7v1"

RUN rm -f /etc/yum.repos.d/*
ADD el7.aliyun.repo /etc/yum.repos.d/base.repo

RUN yum install -y sysvinit-tools psmisc rsync libaio numactl-libs openssl-libs openssh \
    libjpeg libpng libwebp libmcrypt libpg libaspell libexslt libicui18n && \
    yum clean all

RUN useradd -u 2048 -d /home/action action

#ADD rootfs_addon/usr/local/bin /usr/local/bin
#ADD rootfs_addon/usr/share /usr/share

ENV USER action
ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

