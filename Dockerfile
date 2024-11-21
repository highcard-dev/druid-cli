FROM golang:bullseye AS build

COPY . .
COPY .docker/entrypoint.sh /entrypoint.sh

WORKDIR /go

ENV VERSION=docker

RUN make build
RUN make build-plugins

RUN cp ./bin/druid* /usr/bin/

ARG UID=1000
ARG GID=1000
RUN groupadd -g $GID -o druid
RUN useradd -m -u $UID -g $GID -o -s /bin/bash druid

USER druid

ENTRYPOINT [ "/entrypoint.sh" ]