ARG VERSION=latest
FROM highcard/druid:${VERSION}
USER root

RUN apt-get update && apt-get install -y \
    curl xz-utils \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -m 0755 /nix && chown druid /nix
USER druid

# install Nix package manager
RUN bash -c "sh <(curl --proto '=https' --tlsv1.2 -L https://nixos.org/nix/install) --no-daemon"
# Make nix available in PATH for all RUN commands
ENV PATH=/home/druid/.nix-profile/bin:/home/druid/.nix-profile/sbin:$PATH
