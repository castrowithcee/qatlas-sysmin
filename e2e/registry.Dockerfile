# Image registry with HTTPS and basic authentication.
FROM registry:2

COPY certs /certs
COPY htpasswd /auth/htpasswd

ENV REGISTRY_HTTP_ADDR=0.0.0.0:443 \
	REGISTRY_HTTP_TLS_CERTIFICATE=/certs/server.crt \
	REGISTRY_HTTP_TLS_KEY=/certs/server.key \
	REGISTRY_AUTH=htpasswd \
	REGISTRY_AUTH_HTPASSWD_REALM=sysmine2e \
	REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd
