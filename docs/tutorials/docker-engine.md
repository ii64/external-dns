# Setting up ExternalDNS on Docker

This tutorial describes how to setup ExternalDNS for usage within a Docker

## Set up your environment

Install docker as guided by the official docs.

The Docker Engine source driver need to talk with Docker socket so make sure it is accessible.

## Example

You can run ExternalDNS using `docker run`, or put it in docker compose as a service.

Deploy with `docker run`:

```shell
docker run -ti -d --name external-dns --user=root \
    -v /var/run/docker.sock:/var/run/docker.sock external-dns:v0.12.2-mini \
        --source docker-engine \
        --provider <provider> \
        --domain-filter=example.local \
        --log-level=info
```

Deploy with Docker compose:

```yaml
services:
  external-dns:
    image: "external-dns:v0.12.2-mini"
    container_name: external-dns
    user: root
    environment: [] 
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
    command:
      - "--source=docker-engine"
      - "--provider=<provider>"
      - "--domain-filter=example.local"
      - "--log-level=info"
```
