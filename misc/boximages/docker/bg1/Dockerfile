FROM busybox:glibc AS main

# FROM bash:4.4 AS bash
# COPY --from=main / /

RUN adduser -u 2048 -h /home/action action -D -s /bin/bash

WORKDIR /home/action
ENV USER action

