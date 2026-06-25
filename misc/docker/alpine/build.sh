#!/bin/sh

set -e

VERSION=3.23

docker pull --platform linux/amd64 alpine:${VERSION}

docker build --platform linux/amd64 -f Dockerfile.${VERSION} -t sysinner/innerstack-alpine:${VERSION} .

