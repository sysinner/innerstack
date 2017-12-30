FROM centos:centos7 as el7


#RUN rm -f /etc/yum.repos.d/*
#ADD yum.el7.repo /etc/yum.repos.d/base.repo

#RUN yum install -y sysvinit-tools

RUN useradd -u 2048 -d /home/action action


FROM busybox:glibc


ADD build_rootfs_target /

COPY --from=el7 /etc/passwd /etc/passwd
COPY --from=el7 /etc/group /etc/group

COPY --from=el7 /home/action/.bashrc /home/action/.bashrc
# COPY --from=el7 /home/action/.bash_profile /home/action/.bash_profile


ENV USER action
ENV LANG en_US.UTF-8

