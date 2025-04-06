FROM golang:bullseye AS builder

COPY . .
COPY .docker/entrypoint.sh /entrypoint.sh

WORKDIR /go

ENV VERSION=docker

RUN make build
RUN make build-plugins

# The binaries are in ./bin/ directory after build

# Second stage: minimal runtime image
FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

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