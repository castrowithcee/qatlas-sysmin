# Release server: the GitHub releases fake with the signed test releases.
FROM debian:12-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /srv/releases

COPY fakes /usr/local/bin/fakes
COPY certs /certs
COPY releases /srv/releases

ENTRYPOINT ["/usr/local/bin/fakes"]
