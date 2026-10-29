# Helper services: GitHub releases fake, git server, webhook receiver (one binary, started per service).
FROM debian:12-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends git ca-certificates \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /data /srv/git /srv/releases

COPY fakes /usr/local/bin/fakes
COPY certs /certs
COPY releases /srv/releases

ENTRYPOINT ["/usr/local/bin/fakes"]
