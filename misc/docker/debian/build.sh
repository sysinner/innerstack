#!/bin/sh

set -e

VERSION=13

docker pull --platform linux/amd64 debian:13.6-slim

docker build --platform linux/amd64 -f Dockerfile.${VERSION} -t sysinner/innerstack-debian:${VERSION} .

