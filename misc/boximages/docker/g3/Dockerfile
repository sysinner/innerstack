FROM rockylinux:8.6

RUN sed -e 's|^mirrorlist=|#mirrorlist=|g' \
    -e 's|^#baseurl=http://dl.rockylinux.org/$contentdir|baseurl=https://mirrors.aliyun.com/rockylinux|g' \
    /etc/yum.repos.d/Rocky-*.repo 

RUN yum install -y dnf-plugins-core && \
    dnf config-manager --set-enabled powertools && \
    dnf install -y epel-release && \
	dnf install -y https://download.postgresql.org/pub/repos/yum/reporpms/EL-8-x86_64/pgdg-redhat-repo-latest.noarch.rpm && \
	dnf -qy module disable postgresql && \
    dnf install -y vim git \
    gcc gcc-c++ cmake clang llvm ctags upx libtool pam-devel fuse fuse-devel \
    perl \
    ruby \
    nodejs npm \
    yum-utils rpm-build createrepo \
    psmisc redhat-lsb-core net-tools shadow-utils \
    rsync curl wget optipng pngquant \
    python3-pip \
    libaio numactl-libs \
    openssl-devel openssl-libs openssh openssh-server \
    libjpeg libpng libwebp libicu libmcrypt openssl-devel libtool-ltdl \
    expat readline readline-devel pcre-devel \
    aspell gd oniguruma \
    libxml2 libxml2-devel libxslt libxslt-devel openldap-devel libgsasl platform-python-devel \
    postgresql12-libs \
    glibc-langpack-en && \
    dnf clean all && \
    rm -fr /var/cache && \
    rm -fr /var/lib/yum

RUN useradd -u 2048 -d /home/action action && \
    rm -f /etc/ssh/ssh_host_rsa_key /etc/ssh/ssh_host_ecdsa_key /etc/ssh/ssh_host_ed25519_key && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin no/g' /etc/ssh/sshd_config && \
    sed -i 's/GSSAPIAuthentication.*/GSSAPIAuthentication no/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_rsa_key/home\/action\/.ssh\/ssh_host_rsa_key/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_ecdsa_key/home\/action\/.ssh\/ssh_host_ecdsa_key/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_ed25519_key/home\/action\/.ssh\/ssh_host_ed25519_key/g' /etc/ssh/sshd_config && \
    chmod 644 /etc/ssh/sshd_config && \
    sed -i -r 's/^(.*pam_nologin.so)/#\1/' /etc/pam.d/sshd

WORKDIR /home/action
ENV USER=action

ENV LANG=en_US.UTF-8
ENV LC_ALL=en_US.UTF-8

