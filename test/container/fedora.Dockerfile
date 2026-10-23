FROM registry.fedoraproject.org/fedora:44

# Match Ubuntu's ordinary user and explicitly start its manager (QA-16, ADR 0034).
RUN dnf install -y systemd dbus-daemon util-linux procps-ng shadow-utils \
    && dnf clean all \
    && useradd --create-home --uid 1500 tester \
    && mkdir -p /var/lib/systemd/linger \
    && touch /var/lib/systemd/linger/tester \
    && systemctl mask systemd-networkd-wait-online.service NetworkManager-wait-online.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
