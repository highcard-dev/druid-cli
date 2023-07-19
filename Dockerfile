FROM golang:bullseye AS build

COPY . .
COPY cmd/daemon/.docker/entrypoint.sh /entrypoint.sh

RUN make build-daemon
RUN make build-plugins

RUN cp druid* /usr/bin/

ENTRYPOINT [ "/entrypoint.sh" ]