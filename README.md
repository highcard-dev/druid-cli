# Druid CLI

This CLI is a process management tool.
It gives users the ability to launch and observe long running processes.

A good use case is to let it run inside of a docker container. It will give additional insights and management abilities.

This CLI is currently deployed within every deployment at [druid.gg](https://druid.gg).

## Scroll OCI manifest

The Druid CLI uses a **so called Scroll** to get instructions on how to launch and handle the process.
A scroll can also include files required for that purpose.
A Scroll is an OCI Artifact, so it is easy to distribute.

## Features

### Dependency based process runner

The way processes are handled is described in the `scroll.yaml` and is similar to, how Github Actions work, just with the ability to run indefinetly.
Processes can also depend on each other.

### Web Server

The is a web server included, easily have remote control over the process.
There is also websocket support for stdout. TTY is also supported.

### Plugin support

There is the ability to extend the druid CLI with Plugins based on [Go-Plugins](https://github.com/hashicorp/go-plugin).

Example Plugins:

https://github.com/highcard-dev/druid-cli/tree/master/plugin

## Documentation

Read more at https://docs.druid.gg/cli
