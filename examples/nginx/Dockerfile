#Dockerhub does not allow for setting of build args :/
ARG IMAGE=nginx
FROM highcard/druidd-base:latest as base
FROM $IMAGE
WORKDIR /app

RUN apt update
RUN apt install -y ca-certificates wget jq moreutils htop procps nano net-tools gcc make 

RUN wget https://go.dev/dl/go1.21.6.linux-arm64.tar.gz 
RUN tar -C /usr/local -xzf go1.21.6.linux-arm64.tar.gz



#/root/go/bin is not in the path
ENV PATH=$PATH:/root/go/bin
ENV PATH=$PATH:/usr/local/go/bin

#install delve
RUN go install github.com/go-delve/delve/cmd/dlv@latest

RUN wget https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 -O /usr/bin/yq
RUN chmod +x /usr/bin/yq

#RUN wget "https://github.com/highcard-dev/druidd/releases/download/latest/druid_linux_386.deb" -O "druidd.deb"
#RUN apt install ./druidd.deb

RUN useradd -ms /bin/bash druid
USER root

COPY --from=base /usr/bin/druid* /usr/bin
COPY --from=base /entrypoint.sh /entrypoint.sh



ENTRYPOINT [ "/entrypoint.sh" ]