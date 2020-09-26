FROM centos:centos8

RUN rm -f /etc/yum.repos.d/*
ADD yum.el8.repo /etc/yum.repos.d/g3.repo

RUN yum install -y readline

RUN yum install -y vim git \
    gcc gcc-c++ clang ctags upx libtool pam-devel fuse fuse-devel \
    yum-utils rpm-build createrepo \
    psmisc redhat-lsb-core net-tools shadow-utils \
    rsync curl wget optipng pngquant \
    python3-pip \
    libaio numactl-libs \
    openssl-devel openssl-libs openssh openssh-server \
    libjpeg libpng libwebp libicu libmcrypt openssl-devel libtool-ltdl \
    libxslt expat readline-devel pcre-devel \
    postgresql12-libs && \
    yum clean all && \
    rm -fr /var/cache && \
    rm -fr /var/lib/yum


# RUN pip install --upgrade pip && pip install --upgrade yapf

RUN useradd -u 2048 -d /home/action action && \
    rm -f /etc/ssh/ssh_host_rsa_key /etc/ssh/ssh_host_ecdsa_key /etc/ssh/ssh_host_ed25519_key && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin no/g' /etc/ssh/sshd_config && \
    sed -i 's/GSSAPIAuthentication.*/GSSAPIAuthentication no/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_rsa_key/home\/action\/.ssh\/ssh_host_rsa_key/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_ecdsa_key/home\/action\/.ssh\/ssh_host_ecdsa_key/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_ed25519_key/home\/action\/.ssh\/ssh_host_ed25519_key/g' /etc/ssh/sshd_config && \
    chmod 644 /etc/ssh/sshd_config

WORKDIR /home/action
ENV USER action
# ENV localedef -c -f UTF-8
ENV LANG C.UTF-8
ENV LC_CTYPE C.UTF-8
ENV LC_MESSAGES C.UTF-8
ENV LC_ALL C.UTF-8
# ENV LANG en_US.UTF-8
# ENV LC_ALL en_US.UTF-8

