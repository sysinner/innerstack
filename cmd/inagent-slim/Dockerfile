ARG TARGETPLATFORM
ARG BUILDER
FROM --platform=${TARGETPLATFORM} ${BUILDER} AS builder

ARG INAGENT_VERSION

WORKDIR /build

COPY cmd/inagent-slim/third_party/ third_party/
COPY cmd/inagent-slim/src/ src/
COPY cmd/inagent-slim/CMakeLists.txt .

RUN mkdir -p build && cd build && \
    cmake -DCMAKE_BUILD_TYPE=Release -DSTATIC_LINK=ON -DINAGENT_VERSION=${INAGENT_VERSION} .. && \
    make -j$(nproc)
