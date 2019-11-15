FROM ubuntu:18.04

RUN apt update && \
    apt install -y psmisc rsync openssl git vim wget telnet build-essential python3 python3-setuptools python3-pip cmake clang && \
    apt clean all && \
    rm -fr /var/cache/apt

RUN pip3 install --upgrade pip && \
	pip install --upgrade yapf numpy

RUN pip install pandas tensorflow

ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8
