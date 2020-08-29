FROM centos:centos7

ADD yum.el7.repo /etc/yum.repos.d/g2.repo

RUN yum install -y vim git224 \
    gcc gcc-c++ clang ctags upx pref \
    yum-utils rpm-build createrepo \
    psmisc sysvinit-tools redhat-lsb-core net-tools iproute telnet shadow-utils \
    rsync curl wget optipng pngquant \
    python-pip \
    libaio numactl-libs \
    openssl-libs openssh openssh-server \
    libjpeg libpng libwebp libicu libmcrypt libtool-ltdl \
    aspell-devel libxslt expat \
    postgresql12-libs \
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
ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8
