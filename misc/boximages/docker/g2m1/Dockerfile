FROM sysinner/innerstack-g2:el7

ADD yum.el7.repo /etc/yum.repos.d/g2m1.el7.repo

RUN yum install -y ffmpeg && \
    yum clean all && \
    rm -fr /var/cache && \
    rm -fr /var/lib/yum

