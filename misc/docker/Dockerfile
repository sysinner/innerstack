FROM centos:centos7
MAINTAINER Eryx <evorui#gmail.com>

LABEL "docker/image/name"="lessos:d1el7b1"

#RUN rm -f /etc/yum.repos.d/* && \
#    curl -X GET http://mirrors.aliyun.com/repo/Centos-7.repo -o /etc/yum.repos.d/CentOS-Base.repo && \
#    sed -i 's/http:\/\/mirrors.aliyuncs.com.*//g' /etc/yum.repos.d/CentOS-Base.repo

RUN rm -f /etc/yum.repos.d/*
ADD el7.aliyun.repo /etc/yum.repos.d/base.repo

RUN yum install -y vim openssh-server sysvinit-tools psmisc rsync && \
    yum clean all

RUN useradd -u 2048 -d /home/action action

RUN rm -f /etc/ssh/ssh_host_rsa_key /etc/ssh/ssh_host_ecdsa_key /etc/ssh/ssh_host_ed25519_key && \
    ssh-keygen -t rsa -f /etc/ssh/ssh_host_rsa_key -N '' && \
    ssh-keygen -t ecdsa -f /etc/ssh/ssh_host_ecdsa_key -N '' && \
    ssh-keygen -t ed25519 -f /etc/ssh/ssh_host_ed25519_key -N '' && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin no/g' /etc/ssh/sshd_config && \
    sed -i 's/GSSAPIAuthentication.*/GSSAPIAuthentication no/g' /etc/ssh/sshd_config

#RUN mkdir -p /home/action/.ssh && \
#    ssh-keygen -f /home/action/.ssh/id_rsa -N '' && \
#    chown -R action:action /home/action/.ssh

ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

