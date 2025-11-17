FROM golang:bullseye AS builder

ARG VERSION=docker

COPY . .
COPY .docker/entrypoint.sh /entrypoint.sh

WORKDIR /go

ENV VERSION=${VERSION}

RUN make build
RUN make build-plugins

# The binaries are in ./bin/ directory after build

# Second stage: minimal runtime image
FROM ubuntu:24.04

## Remove ubuntu user added in 24.04 by default
RUN touch /var/mail/ubuntu && chown ubuntu /var/mail/ubuntu && userdel -r ubuntu

RUN apt-get update && apt-get install -y \
    ca-certificates wget\
    && rm -rf /var/lib/apt/lists/*

RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then YQ_ARCH="amd64"; \
    elif [ "$ARCH" = "aarch64" ]; then YQ_ARCH="arm64"; \
    else YQ_ARCH="$ARCH"; fi && \
    wget https://github.com/mikefarah/yq/releases/latest/download/yq_linux_${YQ_ARCH} -O /usr/bin/yq && \
    chmod +x /usr/bin/yq

# Copy only the built binaries and entrypoint from builder
COPY --from=builder /go/bin/druid* /usr/bin/
COPY --from=builder /entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Set up user with the same UID/GID
ARG UID=1000
ARG GID=1000
RUN groupadd -g $GID -o druid
RUN useradd -m -u $UID -g $GID -o -s /bin/bash druid

USER druid

ENTRYPOINT [ "/entrypoint.sh" ]