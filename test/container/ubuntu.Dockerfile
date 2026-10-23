FROM docker.io/library/ubuntu:26.04

# A user manager without a desktop login exercises the shipped service (QA-16,
# ADR 0034). Network waits would only time out in the isolated runtime network.
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends systemd systemd-sysv dbus-user-session util-linux procps ca-certificates && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1500 tester \
    && mkdir -p /var/lib/systemd/linger \
    && touch /var/lib/systemd/linger/tester \
    && systemctl mask systemd-networkd-wait-online.service NetworkManager-wait-online.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
