FROM golang:bullseye AS build

COPY . .
COPY .docker/entrypoint.sh /entrypoint.sh

WORKDIR /go

ENV VERSION=docker

RUN make build
RUN make build-plugins

RUN cp ./bin/druid* /usr/bin/


ENTRYPOINT [ "/entrypoint.sh" ]