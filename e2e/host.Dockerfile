# Simulated Linux host: systemd as init, no Docker, and the tools an administrator expects.
FROM debian:12

ENV container=docker DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
	&& apt-get install -y --no-install-recommends systemd systemd-sysv dbus ca-certificates curl \
		openssh-client openssh-server sudo man-db \
	&& rm -rf /var/lib/apt/lists/* \
	&& systemctl enable ssh

# An administrator account the way a real host has one.
RUN useradd --create-home --shell /bin/bash --groups sudo e2e-admin \
	&& echo 'e2e-admin ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/e2e-admin \
	&& chmod 0440 /etc/sudoers.d/e2e-admin

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
