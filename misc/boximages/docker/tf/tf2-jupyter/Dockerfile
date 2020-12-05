FROM tensorflow/tensorflow:2.3.1-jupyter

RUN apt-get update && apt-get install -y --no-install-recommends \
    rsync wget curl openssh-client openssh-server git build-essential vim && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
 
RUN useradd -u 2048 -d /home/action action && \
    rm -f /etc/ssh/ssh_host_rsa_key /etc/ssh/ssh_host_ecdsa_key /etc/ssh/ssh_host_ed25519_key && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin no/g' /etc/ssh/sshd_config && \
    sed -i 's/GSSAPIAuthentication.*/GSSAPIAuthentication no/g' /etc/ssh/sshd_config && \
    sed -i 's/HostKey\ \/etc\/ssh\/ssh_host_dsa_key/#/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_rsa_key/home\/action\/.ssh\/ssh_host_rsa_key/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_ecdsa_key/home\/action\/.ssh\/ssh_host_ecdsa_key/g' /etc/ssh/sshd_config && \
    sed -i 's/etc\/ssh\/ssh_host_ed25519_key/home\/action\/.ssh\/ssh_host_ed25519_key/g' /etc/ssh/sshd_config && \
    echo "\n\nPidFile /home/action/var/run_sshd.pid\n" >> /etc/ssh/sshd_config && \
    mkdir /var/run/sshd && chmod 0755 /var/run/sshd && \
    chmod 644 /etc/ssh/sshd_config

WORKDIR /home/action
ENV USER=action

