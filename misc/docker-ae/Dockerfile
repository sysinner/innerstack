FROM centos:centos7
MAINTAINER Eryx <evorui#gmail.com>

LABEL "docker/image/name"="sysinner:a1el7v1"

RUN rm -f /etc/yum.repos.d/*
ADD el7.aliyun.repo /etc/yum.repos.d/base.repo

RUN yum install -y https://download.postgresql.org/pub/repos/yum/10/redhat/rhel-7-x86_64/pgdg-redhat10-10-1.noarch.rpm && \
    yum install -y sysvinit-tools psmisc rsync libaio numactl-libs openssl-libs openssh libjpeg libpng libwebp libicu libmcrypt libtool-ltdl aspell-devel libxslt postgresql10-libs openssh git2u && \
    yum clean all

RUN useradd -u 2048 -d /home/action action

ENV USER action
ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

