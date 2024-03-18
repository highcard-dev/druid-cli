FROM golang:bullseye AS build

COPY . .
COPY /apps/deployment/daemon/.docker/entrypoint.sh /entrypoint.sh

WORKDIR /go/apps/deployment/daemon

ENV VERSION=docker

RUN make build
RUN make build-plugins

RUN cp ./bin/druid* /usr/bin/


ENTRYPOINT [ "/entrypoint.sh" ]