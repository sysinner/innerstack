FROM centos:centos7
MAINTAINER Eryx <evorui#gmail.com>

LABEL "docker/image/name"="sysinner:a1el7v1"

RUN rm -f /etc/yum.repos.d/*
ADD el7.aliyun.repo /etc/yum.repos.d/base.repo

RUN yum install -y sysvinit-tools psmisc rsync libaio numactl-libs && \
    yum clean all

RUN useradd -u 2048 -d /home/action action

ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

