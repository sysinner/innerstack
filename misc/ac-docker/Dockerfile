FROM centos:centos7 as el7

RUN rm -f /etc/yum.repos.d/*
ADD yum.el7.repo /etc/yum.repos.d/base.repo

RUN yum install -y sysvinit-tools psmisc rsync libaio numactl-libs openssl-libs openssh

RUN useradd -u 2048 -d /home/action action


FROM busybox:glibc

COPY --from=el7 /lib64/libssl.so.10 /lib64/
COPY --from=el7 /lib64/libacl.so.1 /lib64/
COPY --from=el7 /lib64/libpopt.so.0 /lib64/
COPY --from=el7 /lib64/libattr.so.1 /lib64/
COPY --from=el7 /lib64/libutil.so.1 /lib64/
COPY --from=el7 /lib64/libaio.so.1 /lib64/
COPY --from=el7 /lib64/libnuma.so.1 /lib64/
COPY --from=el7 /lib64/libcrypt.so.1 /lib64/
COPY --from=el7 /lib64/libdl.so.2 /lib64/
COPY --from=el7 /lib64/librt.so.1 /lib64/
COPY --from=el7 /lib64/libstdc++.so.6 /lib64/
COPY --from=el7 /lib64/libgcc_s.so.1 /lib64/
COPY --from=el7 /lib64/libfreebl3.so /lib64/
COPY --from=el7 /lib64/libncurses.so.5 /lib64/
COPY --from=el7 /lib64/libtinfo.so.5 /lib64/
COPY --from=el7 /lib64/libselinux.so.1 /lib64/
COPY --from=el7 /lib64/libpcre.so.1 /lib64/


COPY --from=el7 /usr/bin/bash /usr/bin/
COPY --from=el7 /usr/bin/killall /usr/bin/
COPY --from=el7 /usr/bin/ldd /usr/bin/
COPY --from=el7 /usr/bin/rsync /usr/bin/
COPY --from=el7 /usr/sbin/useradd /usr/sbin/
COPY --from=el7 /sbin/nologin /sbin/


COPY --from=el7 /etc/passwd /etc/
COPY --from=el7 /etc/group /etc/
COPY --from=el7 /home/action/.bashrc /home/action/

RUN ln -s /usr/bin/bash /bin/bash

ENV USER action
ENV LANG en_US.UTF-8

